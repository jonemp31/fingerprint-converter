package main

import (
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"fingerprint-converter/internal/cache"
	"fingerprint-converter/internal/config"
	"fingerprint-converter/internal/handlers"
	"fingerprint-converter/internal/pool"
	"fingerprint-converter/internal/services"
)

func main() {
	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("[FingerprintConverter] ")
	log.Println("🚀 Starting Fingerprint Converter API...")

	// Load configuration
	cfg := config.Load()

	// Set runtime optimizations
	runtime.GOMAXPROCS(runtime.NumCPU())
	log.Printf("⚙️  GOMAXPROCS=%d, GOGC=%d, GOMEMLIMIT=%s", 
		runtime.NumCPU(), cfg.GOGC, cfg.GoMemLimit)

	// Initialize buffer pool
	log.Printf("📦 Initializing buffer pool: count=%d, size=%d bytes", 
		cfg.BufferPoolSize, cfg.BufferSize)
	bufferPool := pool.NewBufferPool(cfg.BufferPoolSize, cfg.BufferSize)

	// Initialize worker pool
	log.Printf("👷 Initializing worker pool: workers=%d", cfg.MaxWorkers)
	workerPool := pool.NewWorkerPool(cfg.MaxWorkers)
	if err := workerPool.Start(); err != nil {
		log.Fatalf("❌ Failed to start worker pool: %v", err)
	}

	// Initialize device cache
	var deviceCache *cache.DeviceCache
	if cfg.EnableCache {
		log.Printf("💾 Initializing device cache: dir=%s, cacheTTL=%v, fileTTL=%v",
			cfg.CacheDir, cfg.CacheTTL, cfg.FileTTL)
		deviceCache = cache.NewDeviceCache(cfg.CacheDir, cfg.CacheTTL, cfg.FileTTL)
	} else {
		log.Println("⚠️  Cache disabled")
		// Create dummy cache with 0 TTL
		deviceCache = cache.NewDeviceCache(cfg.CacheDir, 0, 0)
	}

	// Initialize downloader
	downloader := services.NewDownloader(bufferPool, cfg.MaxDownloadSize, cfg.DownloadTimeout)

	// Initialize converters
	audioConverter := services.NewAudioConverter(workerPool, bufferPool)
	imageConverter := services.NewImageConverter(workerPool, bufferPool)
	videoConverter := services.NewVideoConverter(workerPool, bufferPool)

	// Initialize handler
	converterHandler := handlers.NewConverterHandler(
		audioConverter,
		imageConverter,
		videoConverter,
		downloader,
		deviceCache,
		workerPool,
		bufferPool,
		cfg.RequestTimeout,
		cfg.CacheDir,
	)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		ServerHeader:     "FingerprintConverter",
		AppName:          "Fingerprint Media Converter API",
		BodyLimit:        cfg.BodyLimit,
		ReadTimeout:      cfg.ReadTimeout,
		WriteTimeout:     cfg.WriteTimeout,
		DisableKeepalive: false,
		ErrorHandler: func(c fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			message := "Internal Server Error"

			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
				message = e.Message
			}

			return c.Status(code).JSON(fiber.Map{
				"success": false,
				"error":   message,
				"timestamp": time.Now().Unix(),
			})
		},
	})

	// Middleware
	app.Use(recover.New())
	
	if cfg.EnableCORS {
		app.Use(cors.New(cors.Config{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "HEAD", "OPTIONS"},
			AllowHeaders: []string{"Origin", "Content-Type", "Accept"},
		}))
	}

	if cfg.EnablePerformanceLogs {
		app.Use(logger.New(logger.Config{
			Format: "[${time}] ${status} - ${latency} ${method} ${path}\n",
		}))
	}

	// Routes
	api := app.Group("/api")

	// Conversion endpoint
	api.Post("/convert", converterHandler.Convert)

	// Cache stats
	api.Get("/cache/stats", converterHandler.GetCacheStats)
	api.Get("/cache/stats/:deviceID", converterHandler.GetCacheStats)

	// Health check
	if cfg.EnableHealthCheck {
		api.Get("/health", converterHandler.Health)
	}

	// Root endpoint
	app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service":  "Fingerprint Media Converter API",
			"version":  "1.0.0",
			"status":   "running",
			"endpoints": []string{
				"POST /api/convert",
				"GET  /api/cache/stats",
				"GET  /api/cache/stats/:deviceID",
				"GET  /api/health",
			},
		})
	})

	// Graceful shutdown
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("🛑 Shutting down gracefully...")

		// Stop worker pool
		workerPool.Stop()

		// Stop cache cleanup
		deviceCache.Stop()

		// Shutdown Fiber
		if err := app.Shutdown(); err != nil {
			log.Printf("⚠️  Error during shutdown: %v", err)
		}

		log.Println("👋 Goodbye!")
		os.Exit(0)
	}()

	// Start server
	log.Printf("🌐 Server starting on port %s", cfg.Port)
	log.Printf("🎯 Environment: %s", cfg.AppEnv)
	log.Printf("📊 Anti-Fingerprint Default Level: %s", cfg.DefaultAFLevel)
	log.Println("✅ Ready to process media!")

	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("❌ Failed to start server: %v", err)
	}
}
