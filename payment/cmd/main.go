package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	paymentService "github.com/Andrew1996-la/stellar-works/payment/pkg/service"
	paymentv1 "github.com/Andrew1996-la/stellar-works/shared/pkg/proto/payment/v1"
)

const (
	grpcAddress = ":50052"

	grpcMaxConnectionIdle     = 15 * time.Minute // Закрыть Idle соединения (нет активных RPC)
	grpcMaxConnectionAge      = 30 * time.Minute // Принудительная ротация при балансировке
	grpcMaxConnectionAgeGrace = 5 * time.Second  // Время на завершения активных RPC
	grpcKeepaliveTime         = 5 * time.Minute  // Интервал ping'ов для обнаружения мёртвых соединений
	grpcKeepaliveTimeout      = 1 * time.Second  // Таймаут ожидания pong
	grpcMinPingInterval       = 5 * time.Minute  // Минимальный интервал ping'ов от клиента (защита от DoS)
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var lc net.ListenConfig

	lis, err := lc.Listen(ctx, "tcp", grpcAddress)
	if err != nil {
		slog.Error("не удалось создать listener", "error", err)
		return
	}

	grpcServer := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     grpcMaxConnectionIdle,
			MaxConnectionAge:      grpcMaxConnectionAge,
			MaxConnectionAgeGrace: grpcMaxConnectionAgeGrace,
			Time:                  grpcKeepaliveTime,
			Timeout:               grpcKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             grpcMinPingInterval,
			PermitWithoutStream: true,
		}),
	)

	paymentv1.RegisterPaymentServiceServer(grpcServer, paymentService.NewServer())

	// Включаем reflection для postman/grpcurl
	reflection.Register(grpcServer)

	slog.Info("запуск PaymentService")

	go func() {
		slog.Info("grpc сервис Payment запущен", "address", grpcAddress)
		if err = grpcServer.Serve(lis); err != nil {
			slog.Error("ошибка запуска сервера", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("остановка grpc сервиса Payment")
	grpcServer.GracefulStop()
	slog.Info("grpc сервис Payment остановлен")
}
