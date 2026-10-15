package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/photo-nini/backend/internal/config"
	"github.com/photo-nini/backend/internal/database"
	"github.com/photo-nini/backend/internal/filestore"
	"github.com/photo-nini/backend/internal/httpapi"
	"github.com/photo-nini/backend/internal/security"
	"github.com/photo-nini/backend/internal/service"
	"github.com/photo-nini/backend/internal/store"
)

// main 负责加载环境配置、验证依赖、装配分层组件并执行可优雅退出的 HTTP 服务。
func main() {
	if err := run(); err != nil {
		// 只记录错误类型，避免数据源或 Redis 的错误文本泄露连接凭据。
		log.Printf("服务启动或运行失败，错误类型=%T", err)
		os.Exit(1)
	}
}

// run 将进程生命周期集中在一处，确保 PostgreSQL、Redis 连接池能在退出时释放。
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()
	databaseConnection, sqlConnection, err := database.OpenPostgres(startupContext, cfg)
	if err != nil {
		return err
	}
	defer sqlConnection.Close()

	repository := store.NewPostgresStore(databaseConnection)
	runtimeConfigService := service.NewRuntimeConfigService(repository)
	runtimeConfig, err := runtimeConfigService.Load(startupContext, cfg)
	if err != nil {
		return err
	}
	fileManager, err := filestore.NewManager(runtimeConfig.LocalStorageRoot)
	if err != nil {
		return err
	}
	switch runtimeConfig.Mode {
	case "release":
		gin.SetMode(gin.ReleaseMode)
	case "debug":
		gin.SetMode(gin.DebugMode)
	case "test":
		gin.SetMode(gin.TestMode)
	}

	tokens := store.NewTokenStore(runtimeConfig)
	defer tokens.Close()
	if err := tokens.Client().Ping(startupContext).Err(); err != nil {
		return errors.New("Redis 启动连通性检查失败")
	}

	passwords, err := security.NewPasswordManager()
	if err != nil {
		return err
	}
	inspirations := service.NewInspirationService(repository, fileManager, runtimeConfig.MaxUploadBytes)
	dependencies := httpapi.Dependencies{
		Auth:                 service.NewAuthService(repository, tokens, passwords),
		Users:                service.NewUserService(repository),
		Tags:                 service.NewTagService(repository),
		Inspirations:         inspirations,
		Tokens:               tokens,
		PostgresPing:         func(ctx context.Context) error { return database.Ping(ctx, databaseConnection) },
		RedisPing:            func(ctx context.Context) error { return tokens.Client().Ping(ctx).Err() },
		MaxBodyBytes:         runtimeConfig.MaxJSONBodyBytes,
		Files:                fileManager,
		MaxUploadBytes:       runtimeConfig.MaxUploadBytes,
		CurrentFileStoreType: runtimeConfigService.CurrentFileStoreType,
		PublicConfigs:        runtimeConfigService,
	}
	router := httpapi.Router(dependencies)
	server := &http.Server{
		Addr:              runtimeConfig.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: runtimeConfig.HTTPReadHeaderTimeout,
		ReadTimeout:       runtimeConfig.HTTPReadTimeout,
		WriteTimeout:      runtimeConfig.HTTPWriteTimeout,
		IdleTimeout:       runtimeConfig.HTTPIdleTimeout,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverError := make(chan error, 1)
	go func() {
		log.Printf("HTTP 服务已启动，监听地址 %s", runtimeConfig.HTTPAddr)
		serverError <- server.ListenAndServe()
	}()
	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-shutdownContext.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
		if err := inspirations.WaitForCompression(shutdown); err != nil {
			return err
		}
		return nil
	}
}
