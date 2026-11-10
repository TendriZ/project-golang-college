package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"api-student/app/model"
)

// Sentinel error khusus portofolio. ErrNotFound dan ErrDuplicate dipakai ulang
// dari paket induk agar service hanya mengenal satu keluarga error.
var (
	ErrInvalidID   = errors.New("format id tidak valid")
	ErrNotOwner    = errors.New("bukan pemilik data")
	ErrProjectLimit = errors.New("jumlah proyek sudah mencapai batas")
)

// MaxProjectsPerPortfolio membatasi pertumbuhan array projects. Satu dokumen
// MongoDB maksimal 16 MB; array yang dibiarkan tumbuh tanpa batas suatu saat
// akan menabrak batas itu.
const MaxProjectsPerPortfolio = 20

// PortfolioRepository adalah kontrak penyimpanan portofolio di MongoDB.
type PortfolioRepository interface {
	EnsureIndexes(ctx context.Context) error
	FindByStudent(ctx context.Context, studentID int) (model.Portfolio, error)
	Upsert(ctx context.Context, p model.Portfolio) (model.Portfolio, error)
	Replace(ctx context.Context, studentID int, p model.Portfolio) (model.Portfolio, error)
	AddProject(ctx context.Context, studentID int, project model.Project) (model.Portfolio, error)
	DeleteProject(ctx context.Context, studentID int, projectID string) (model.Portfolio, error)
	StatsTechnologies(ctx context.Context) (model.PortfolioStats, error)
}

// portfolioDocument adalah bentuk portofolio SEBAGAIMANA TERSIMPAN di MongoDB.
//
// Tag bson menentukan nama field di database; tag json tidak diperlukan karena
// struct ini tidak pernah dikirim ke client. Pemisahan ini membuat package
// model tetap bebas dari tipe-tipe milik driver.
type portfolioDocument struct {
	ID          bson.ObjectID    `bson:"_id,omitempty"`
	StudentID   int              `bson:"student_id"`
	Headline    string           `bson:"headline"`
	Bio         string           `bson:"bio"`
	Projects    []projectDocument `bson:"projects"`
	Certificates []certificateDocument `bson:"certificates"`
	CreatedAt   time.Time        `bson:"created_at"`
	UpdatedAt   time.Time        `bson:"updated_at"`
}

type projectDocument struct {
	ID        bson.ObjectID `bson:"_id"`
	Judul     string        `bson:"judul"`
	Teknologi []string      `bson:"teknologi"`
	Tautan    string        `bson:"tautan"`
	Tahun     int           `bson:"tahun"`
}

type certificateDocument struct {
	ID        bson.ObjectID `bson:"_id"`
	Nama      string        `bson:"nama"`
	Penerbit  string        `bson:"penerbit"`
	Tanggal   time.Time     `bson:"tanggal"`
}

func (d portfolioDocument) toModel() model.Portfolio {
	projects := make([]model.Project, 0, len(d.Projects))
	for _, p := range d.Projects {
		projects = append(projects, model.Project{
			ID:        p.ID.Hex(),
			Judul:     p.Judul,
			Teknologi: p.Teknologi,
			Tautan:    p.Tautan,
			Tahun:     p.Tahun,
		})
	}

	certificates := make([]model.Certificate, 0, len(d.Certificates))
	for _, c := range d.Certificates {
		certificates = append(certificates, model.Certificate{
			ID:        c.ID.Hex(),
			Nama:      c.Nama,
			Penerbit:  c.Penerbit,
			Tanggal:   c.Tanggal,
		})
	}

	return model.Portfolio{
		ID:          d.ID.Hex(),
		StudentID:   d.StudentID,
		Headline:    d.Headline,
		Bio:         d.Bio,
		Projects:    projects,
		Certificates: certificates,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

type portfolioMongoRepository struct {
	coll *mongo.Collection
}

func NewPortfolioRepository(db *mongo.Database) PortfolioRepository {
	return &portfolioMongoRepository{coll: db.Collection("student_portfolios")}
}

// now membulatkan waktu ke milidetik. Tipe date pada BSON hanya menyimpan
// sampai milidetik, sedangkan time.Time di Go menyimpan sampai nanodetik.
func now() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

func parseID(id string) (bson.ObjectID, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return bson.ObjectID{}, ErrInvalidID
	}
	return oid, nil
}

// EnsureIndexes dijalankan sekali saat aplikasi menyala. Membuat index yang
// sudah ada tidak melakukan apa pun, sehingga aman dipanggil berulang kali.
func (r *portfolioMongoRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// Satu mahasiswa hanya boleh memiliki SATU portofolio.
			// Aturan ini dijaga database (unique index), bukan hanya di kode.
			Keys:    bson.D{{Key: "student_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("student_id_unique"),
		},
		{
			// Mendukung agregasi statistik teknologi. Multikey pada field
			// array projects.teknologi agar pencarian teknologi tidak perlu
			// membuka setiap dokumen.
			Keys:    bson.D{{Key: "projects.teknologi", Value: 1}},
			Options: options.Index().SetName("projects_teknologi"),
		},
	})
	if err != nil {
		return fmt.Errorf("membuat index student_portfolios: %w", err)
	}
	return nil
}

// FindByStudent mengambil portofolio milik satu mahasiswa.
func (r *portfolioMongoRepository) FindByStudent(
	ctx context.Context, studentID int,
) (model.Portfolio, error) {
	var doc portfolioDocument
	if err := r.coll.FindOne(ctx, bson.M{"student_id": studentID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.Portfolio{}, ErrNotFound
		}
		return model.Portfolio{}, fmt.Errorf("mengambil portofolio: %w", err)
	}
	return doc.toModel(), nil
}

// Upsert membuat portofolio baru atau mengganti yang sudah ada.
// Digunakan oleh PUT /students/:id/portfolio (create or replace).
func (r *portfolioMongoRepository) Upsert(
	ctx context.Context, p model.Portfolio,
) (model.Portfolio, error) {
	t := now()
	doc := portfolioDocument{
		StudentID:   p.StudentID,
		Headline:    p.Headline,
		Bio:         p.Bio,
		Projects:    []projectDocument{},
		Certificates: []certificateDocument{},
		CreatedAt:   t,
		UpdatedAt:   t,
	}
	if doc.Projects == nil {
		doc.Projects = []projectDocument{}
	}
	if doc.Certificates == nil {
		doc.Certificates = []certificateDocument{}
	}

	filter := bson.M{"student_id": p.StudentID}
	update := bson.M{
		"$set": bson.M{
			"headline":    p.Headline,
			"bio":         p.Bio,
			"projects":    doc.Projects,
			"certificates": doc.Certificates,
			"updated_at":  t,
		},
		"$setOnInsert": bson.M{
			"created_at": t,
		},
	}

	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)

	var updated portfolioDocument
	err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updated)
	if err != nil {
		return model.Portfolio{}, fmt.Errorf("mengganti portofolio: %w", err)
	}
	return updated.toModel(), nil
}

// Replace mengganti seluruh isi portofolio yang sudah ada (non-upsert).
// Digunakan bila ingin menolak bila portofolio belum ada.
func (r *portfolioMongoRepository) Replace(
	ctx context.Context, studentID int, p model.Portfolio,
) (model.Portfolio, error) {
	t := now()
	doc := portfolioDocument{
		StudentID:   studentID,
		Headline:    p.Headline,
		Bio:         p.Bio,
		Projects:    []projectDocument{},
		Certificates: []certificateDocument{},
		UpdatedAt:   t,
	}
	if doc.Projects == nil {
		doc.Projects = []projectDocument{}
	}
	if doc.Certificates == nil {
		doc.Certificates = []certificateDocument{}
	}

	var updated portfolioDocument
	err := r.coll.FindOneAndUpdate(ctx,
		bson.M{"student_id": studentID},
		bson.M{"$set": bson.M{
			"headline":    p.Headline,
			"bio":         p.Bio,
			"projects":    doc.Projects,
			"certificates": doc.Certificates,
			"updated_at":  t,
		}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if err == nil {
		return updated.toModel(), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return model.Portfolio{}, fmt.Errorf("mengganti portofolio: %w", err)
	}
	return model.Portfolio{}, ErrNotFound
}

// AddProject menambahkan satu proyek ke dalam array projects.
//
// Syarat "projects.19 tidak ada" berarti array belum berisi 20 elemen
// (indeks 0 sampai 19). Pemeriksaan batas dan penambahan terjadi dalam satu
// operasi, sehingga dua permintaan serentak tidak dapat sama-sama lolos ketika
// tinggal satu tempat tersisa.
func (r *portfolioMongoRepository) AddProject(
	ctx context.Context, studentID int, project model.Project,
) (model.Portfolio, error) {
	doc := projectDocument{
		ID:        bson.NewObjectID(),
		Judul:     project.Judul,
		Teknologi: project.Teknologi,
		Tautan:    project.Tautan,
		Tahun:     project.Tahun,
	}

	limitField := "projects." + strconv.Itoa(MaxProjectsPerPortfolio-1)

	var updated portfolioDocument
	err := r.coll.FindOneAndUpdate(ctx,
		bson.M{"student_id": studentID, limitField: bson.M{"$exists": false}},
		bson.M{"$push": bson.M{"projects": doc}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if err == nil {
		return updated.toModel(), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return model.Portfolio{}, fmt.Errorf("menambah proyek: %w", err)
	}

	count, err := r.coll.CountDocuments(ctx, bson.M{"student_id": studentID})
	if err != nil {
		return model.Portfolio{}, fmt.Errorf("memeriksa portofolio: %w", err)
	}
	if count == 0 {
		return model.Portfolio{}, ErrNotFound
	}
	return model.Portfolio{}, ErrProjectLimit
}

// DeleteProject menghapus satu proyek dari array projects dengan $pull.
//
// Sebelum menarik, pastikan proyek benar-benar ada dalam array. Karena
// $pull yang tidak cocok masih mengembalikan dokumen (tidak error),
// kita harus memeriksa keberadaan project ID terlebih dahulu agar
// menghapus proyek yang tidak ada mengembalikan 404, bukan 200.
func (r *portfolioMongoRepository) DeleteProject(
	ctx context.Context, studentID int, projectID string,
) (model.Portfolio, error) {
	oid, err := parseID(projectID)
	if err != nil {
		return model.Portfolio{}, err
	}

	// Pastikan portofolio ada dan proyek yang dimaksud terdaftar di dalamnya.
	var current portfolioDocument
	if err := r.coll.FindOne(ctx, bson.M{"student_id": studentID}).Decode(&current); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.Portfolio{}, ErrNotFound
		}
		return model.Portfolio{}, fmt.Errorf("memeriksa portofolio: %w", err)
	}
	found := false
	for _, p := range current.Projects {
		if p.ID == oid {
			found = true
			break
		}
	}
	if !found {
		return model.Portfolio{}, ErrNotFound
	}

	var updated portfolioDocument
	err = r.coll.FindOneAndUpdate(ctx,
		bson.M{"student_id": studentID},
		bson.M{"$pull": bson.M{"projects": bson.M{"_id": oid}}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if err != nil {
		return model.Portfolio{}, fmt.Errorf("menghapus proyek: %w", err)
	}
	return updated.toModel(), nil
}

// StatsTechnologies menghasilkan 10 teknologi paling banyak dipakai di seluruh
// portofolio dengan aggregation pipeline (dua kali $unwind).
func (r *portfolioMongoRepository) StatsTechnologies(
	ctx context.Context,
) (model.PortfolioStats, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{}}},
		{{Key: "$unwind", Value: "$projects"}},
		{{Key: "$unwind", Value: "$projects.teknologi"}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$projects.teknologi",
			"total": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "total", Value: -1}, {Key: "_id", Value: 1}}}},
		{{Key: "$limit", Value: int64(10)}},
	}

	cursor, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return model.PortfolioStats{}, fmt.Errorf("menghitung statistik teknologi: %w", err)
	}
	defer cursor.Close(ctx)

	var out []struct {
		ID     string `bson:"_id"`
		Total  int    `bson:"total"`
	}
	if err := cursor.All(ctx, &out); err != nil {
		return model.PortfolioStats{}, fmt.Errorf("membaca statistik teknologi: %w", err)
	}

	stats := model.PortfolioStats{
		Technologies: []model.TechCount{},
	}
	for _, t := range out {
		stats.Technologies = append(stats.Technologies, model.TechCount{
			Teknologi: t.ID,
			Total:     t.Total,
		})
	}
	return stats, nil
}
