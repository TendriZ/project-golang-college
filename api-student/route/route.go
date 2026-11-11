package route

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"api-student/app/service"
	"api-student/helper"
	"api-student/middleware"
)

type Dependencies struct {
	Pool                *pgxpool.Pool
	Mongo               *mongo.Client
	JWT                 *helper.JWTManager
	Permissions         *helper.PermissionSet
	StudentService      *service.StudentService
	SupervisionService  *service.SupervisionService
	AuthService         *service.AuthService
	PortfolioService    *service.PortfolioService
	NotificationService *service.NotificationService
}

func Register(app *fiber.App, deps Dependencies) {
	api := app.Group("/api/v1")

	// Publik — tetap tanpa token (Tugas 3: /health tetap publik)
	api.Get("/health", healthCheck(deps.Pool, deps.Mongo))

	// Autentikasi
	auth := api.Group("/auth", middleware.RequireJSON)
	auth.Post("/register", deps.AuthService.Register)
	auth.Post("/login", middleware.LoginRateLimiter(), deps.AuthService.Login)
	auth.Post("/refresh", deps.AuthService.Refresh)
	auth.Post("/logout", deps.AuthService.Logout)
	// /auth/me wajib token — beda pesan expired vs invalid ditangani di RequireAuth
	auth.Get("/me", middleware.RequireAuth(deps.JWT), deps.AuthService.Me)

	// Seluruh endpoint students wajib membawa access token.
	// Hak yang dapat diputuskan tanpa melihat data -> middleware.
	// Hak yang bergantung pada kepemilikan data (owner_id) -> di service.
	students := api.Group("/students",
		middleware.RequireJSON, middleware.RequireAuth(deps.JWT))

	perms := deps.Permissions

	// Hak dapat diputuskan tanpa melihat data -> middleware.
	students.Get("/",
		middleware.RequirePermission(perms, "student:list"),
		deps.StudentService.List)
	students.Post("/",
		middleware.RequirePermission(perms, "student:create"),
		deps.StudentService.Create)
	students.Delete("/:id",
		middleware.RequirePermission(perms, "student:delete"),
		deps.StudentService.Delete)

	// Hak bergantung pada kepemilikan data -> diperiksa di service
	// (pemilik selalu boleh; selain itu perlu permission :any).
	students.Get("/:id", deps.StudentService.Get)
	students.Put("/:id", deps.StudentService.Replace)
	students.Patch("/:id", deps.StudentService.Patch)

	// --- portofolio (MongoDB) ---
	// Semua endpoint portofolio di bawah grup /students sudah mewarisi
	// RequireJSON dan RequireAuth dari grup induk. Service memeriksa
	// keberadaan mahasiswa di PostgreSQL sebelum menyentuh MongoDB.
	portfolio := students.Group("/:id/portfolio")
	portfolio.Get("/", deps.PortfolioService.Get)
	portfolio.Put("/", deps.PortfolioService.Upsert)
	portfolio.Post("/projects", deps.PortfolioService.AddProject)
	portfolio.Delete("/projects/:projectId", deps.PortfolioService.DeleteProject)

	// Statistik teknologi gabungan (melintasi seluruh portofolio).
	portfolios := api.Group("/portfolios",
		middleware.RequireJSON,
		middleware.RequireAuth(deps.JWT))
	portfolios.Get("/stats/technologies", deps.PortfolioService.StatsTechnologies)

	// --- pembimbing tugas akhir ---
	// Seluruh endpoint di bawah ini adalah operasi administratif:
	// keputusannya tidak bergantung pada pemilik data, sehingga cukup
	// dijaga middleware permission supervision:manage.
	supervisors := api.Group("/supervisors",
		middleware.RequireJSON, middleware.RequireAuth(deps.JWT))
	supervisors.Post("/:id/assign",
		middleware.RequirePermission(perms, "supervision:manage"),
		deps.SupervisionService.Assign)
	supervisors.Patch("/:id",
		middleware.RequirePermission(perms, "supervision:manage"),
		deps.SupervisionService.UpdateQuota)

	supervisions := api.Group("/supervisions",
		middleware.RequireJSON, middleware.RequireAuth(deps.JWT))
	supervisions.Post("/switch",
		middleware.RequirePermission(perms, "supervision:manage"),
		deps.SupervisionService.Switch)

	// --- notifikasi ---
	// Setiap pengguna hanya boleh membaca notifikasi miliknya sendiri.
	// Kepemilikan ditentukan dari token, bukan dari parameter URL.
	api.Get("/notifications/mine",
		middleware.RequireAuth(deps.JWT),
		deps.NotificationService.Mine)

	// Pemantauan outbox dan queue adalah operasi administratif.
	admin := api.Group("/admin",
		middleware.RequireJSON, middleware.RequireAuth(deps.JWT))
	admin.Get("/queue",
		middleware.RequirePermission(perms, "queue:monitor"),
		deps.NotificationService.Monitor)
}

// healthCheck kini memeriksa DUA database. Aplikasi dianggap sehat hanya
// bila keduanya dapat dihubungi, dan pesan kegagalannya menyebut yang mana.
func healthCheck(pool *pgxpool.Pool, mongoClient *mongo.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			return helper.ServiceUnavailable("PostgreSQL tidak dapat dihubungi")
		}

		if err := mongoClient.Ping(ctx, nil); err != nil {
			return helper.ServiceUnavailable("MongoDB tidak dapat dihubungi")
		}

		return helper.Success(
			c,
			fiber.StatusOK,
			"server, PostgreSQL, dan MongoDB berjalan",
			nil,
		)
	}
}
