package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	paymentv1 "github.com/Andrew1996-la/stellar-works/shared/pkg/proto/payment/v1"
)

// server реализует gRPC сервис оплаты
type server struct {
	paymentv1.UnimplementedPaymentServiceServer
}

// NewServer создаёт новый экземпляр сервера оплаты
func NewServer() *server {
	return &server{}
}

// PayOrder обрабатывает оплату заказа
func (s *server) PayOrder(
	ctx context.Context,
	req *paymentv1.PayOrderRequest,
) (*paymentv1.PayOrderResponse, error) {
	// Проверить, что order_uuid не пустой → INVALID_ARGUMENT
	if req.GetOrderUuid() == "" {
		return nil, status.Error(codes.InvalidArgument, "order_uuid обязателен")
	}
	// Проверить, что payment_method != UNSPECIFIED → INVALID_ARGUMENT
	if req.GetPaymentMethod() == paymentv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "способ оплаты обязателен")
	}

	// Проверить формат UUID → INVALID_ARGUMENT
	if _, err := uuid.Parse(req.GetOrderUuid()); err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"неверный формат order_uuid: %s",
			req.GetOrderUuid(),
		)
	}

	// Сгенерировать transaction_uuid (UUID v4)
	transactionUUID := uuid.NewString()

	// Вывести в лог: "оплата прошла успешно, order_uuid: X, transaction_uuid: Y"
	slog.Info(
		"оплата прошла успешно",
		"order_uuid", req.GetOrderUuid(),
		"transaction_uuid", transactionUUID,
	)
	// Вернуть transaction_uuid
	return &paymentv1.PayOrderResponse{
		TransactionUuid: transactionUUID,
	}, nil
}
