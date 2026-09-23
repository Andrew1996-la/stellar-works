package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	orderHandler "github.com/Andrew1996-la/stellar-works/order/pkg/handler"
	inventoryv1 "github.com/Andrew1996-la/stellar-works/shared/pkg/proto/inventory/v1"
	paymentv1 "github.com/Andrew1996-la/stellar-works/shared/pkg/proto/payment/v1"
)

const (
	httpPort                = "8080"
	inventoryServiceAddress = "localhost:50051"
	paymentServiceAddress   = "localhost:50052"

	// Таймауты для HTTP-сервера
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	grpcClientOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second, // Интервал ping для обнаружения мертвых соединений
			Timeout:             3 * time.Second,  // timeout ожидания pong
			PermitWithoutStream: true,             // держать соединения теплым без активных rpc
		}),
	}

	// Создание gRPC соединение с InventoryService
	inventoryConn, err := grpc.NewClient(inventoryServiceAddress, grpcClientOptions...)
	if err != nil {
		slog.Error("не удалось подключиться к InventoryService", "error", err)
		return
	}
	defer inventoryConn.Close()

	// Создание gRPC клиент PaymentService
	paymentConn, err := grpc.NewClient(paymentServiceAddress, grpcClientOptions...)
	if err != nil {
		slog.Error("не удалось подключиться к PaymentService", "error", err)
		return
	}
	defer paymentConn.Close()

	// Создаём хранилище и обработчик
	store := orderHandler.NewOrderStore()
	h := orderHandler.NewHandler(
		inventoryv1.NewInventoryServiceClient(inventoryConn),
		paymentv1.NewPaymentServiceClient(paymentConn),
		store,
	)

	// Создать OpenAPI сервер
	orderServer, err := orderHandler.SetupServer(h)
	if err != nil {
		slog.Error("ошибка создания сервера OpenAPI", "error", err)
		return
	}

	server := &http.Server{
		Addr:              net.JoinHostPort("localhost", httpPort),
		Handler:           orderServer,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	// graceful shutdown для HTTP сервера
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		slog.Info("запуск OrderService", "port", httpPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("ошибка запуска сервера", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	// Создаем контекст с таймаутом для остановки сервера
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("ошибка при остановке сервера", "error", err)
	}

	slog.Info("http сервис Order остановлен")
}
