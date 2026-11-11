package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"api-student/app/repository"
	"api-student/app/service"
	"api-student/config"
	"api-student/database"
	"api-student/helper"
	"api-student/route"
)

const minSecretLength = 32

func main() {
	role := flag.String("role", "all", "komponen yang dijalankan: api, worker, atau all")
	flag.Parse()

	if *role != "api" && *role != "worker" && *role != "all" {
		fmt.Fprintf(os.Stderr, "role tidak dikenal: %q (pilih api, worker, atau all)\n", *role)
		os.Exit(2)
	}

	config.LoadEnv()
	logger := config.NewLogger()

	// Satu context untuk seluruh proses. SIGINT/SIGTERM membatalkannya,
	// sehingga relay berhenti dan server HTTP / worker Asynq dapat
	// dimatikan dengan rapi.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.NewPool(ctx)
	if err != nil {
		logger.Error("gagal terhubung ke database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	// Satu pintu untuk seluruh transaction pada aplikasi ini.
	txManager := repository.NewTxManager(pool)

	// Worker tidak membutuhkan Mongo maupun JWT: ia hanya membaca outbox,
	// menulis notifications, dan mengirim email. Memisahkan peran membuat
	// worker dapat dijalankan sebagai proses tersendiri.
	var stopWorker func()
	if *role == "worker" || *role == "all" {
		stopWorker = startWorker(ctx, logger, pool)
		if stopWorker == nil {
			os.Exit(1)
		}
		defer stopWorker()
	}

	if *role == "api" || *role == "all" {
		runAPI(ctx, logger, pool, txManager)
	} else {
		<-ctx.Done()
	}

	logger.Info("aplikasi berhenti dengan rapi")
}

// runAPI merakit dan menjalankan seluruh komponen HTTP. Fungsi ini baru
// kembali setelah context dibatalkan dan server selesai ditutup.
func runAPI(
	ctx context.Context,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	txManager *repository.TxManager,
) {
	jwtSecret := config.GetEnv("JWT_SECRET", "")
	if len(jwtSecret) < minSecretLength {
		logger.Error("JWT_SECRET tidak diisi atau terlalu pendek",
			slog.Int("minimal_karakter", minSecretLength))
		os.Exit(1)
	}

	mongoClient, err := database.NewMongoClient(ctx)
	if err != nil {
		logger.Error("gagal terhubung ke MongoDB", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = mongoClient.Disconnect(shutdownCtx)
	}()
	mongoDB := mongoClient.Database(config.GetEnv("MONGO_DATABASE", "db_students"))

	jwtManager := helper.NewJWTManager(
		jwtSecret,
		config.GetEnv("JWT_ISSUER", "praktikum-backend"),
		time.Duration(config.GetEnvInt("JWT_ACCESS_TTL_MINUTES", 15))*time.Minute,
	)

	studentRepository := repository.NewStudentRepository(pool)
	userRepository := repository.NewUserRepository(pool)
	tokenRepository := repository.NewTokenRepository(pool)
	roleRepository := repository.NewRoleRepository(pool)
	supervisorRepository := repository.NewSupervisorRepository(pool)
	supervisionRepository := repository.NewSupervisionRepository(pool)
	outboxRepository := repository.NewOutboxRepository(pool)
	notificationRepository := repository.NewNotificationRepository(pool)

	// Pemetaan role ke permission dibaca SEKALI saat aplikasi menyala.
	// Konsekuensinya: perubahan hak akses di database baru berlaku setelah
	// aplikasi dijalankan ulang. Itu keputusan sadar, bukan kelalaian.
	rawPermissions, err := roleRepository.LoadPermissions(context.Background())
	if err != nil {
		logger.Error("gagal memuat permission", slog.String("error", err.Error()))
		os.Exit(1)
	}
	permissions := helper.NewPermissionSet(rawPermissions)
	logger.Info("permission dimuat", slog.Any("roles", permissions.KnownRoles()))

	studentService := service.NewStudentService(studentRepository, permissions)
	supervisionService := service.NewSupervisionService(
		studentRepository, supervisorRepository, supervisionRepository,
		outboxRepository, txManager,
	)
	authService := service.NewAuthService(
		userRepository, tokenRepository, jwtManager,
		time.Duration(config.GetEnvInt("JWT_REFRESH_TTL_DAYS", 7))*24*time.Hour,
		permissions,
	)

	portfolioRepository := repository.NewPortfolioRepository(mongoDB)
	if err := portfolioRepository.EnsureIndexes(context.Background()); err != nil {
		logger.Error("gagal membuat index MongoDB", slog.String("error", err.Error()))
		os.Exit(1)
	}
	portfolioService := service.NewPortfolioService(portfolioRepository, studentRepository, permissions)

	inspector := asynq.NewInspector(config.RedisOpt())
	defer inspector.Close()
	notificationService := service.NewNotificationService(
		notificationRepository, outboxRepository, inspector,
	)

	app := config.NewApp(logger, route.Dependencies{
		Pool:                pool,
		Mongo:               mongoClient,
		JWT:                 jwtManager,
		Permissions:         permissions,
		StudentService:      studentService,
		SupervisionService:  supervisionService,
		AuthService:         authService,
		PortfolioService:    portfolioService,
		NotificationService: notificationService,
	})

	port := config.GetEnv("APP_PORT", "3000")

	go func() {
		if err := app.Listen(":" + port); err != nil {
			logger.Error("server berhenti", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	logger.Info("server berjalan", slog.String("port", port))

	<-ctx.Done()
	logger.Info("sinyal berhenti diterima, menutup server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		logger.Error("gagal menutup server dengan rapi", slog.String("error", err.Error()))
	}
}
