package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orderv1 "github.com/Andrew1996-la/stellar-works/shared/pkg/openapi/order/v1"
	inventoryv1 "github.com/Andrew1996-la/stellar-works/shared/pkg/proto/inventory/v1"
	paymentv1 "github.com/Andrew1996-la/stellar-works/shared/pkg/proto/payment/v1"
)

// timeout для контекста grpc
const grpcTimeout = 5 * time.Second

// OrderStatus — статус заказа
type OrderStatus string

const (
	OrderStatusPendingPayment OrderStatus = "PENDING_PAYMENT"
	OrderStatusPaid           OrderStatus = "PAID"
	OrderStatusCancelled      OrderStatus = "CANCELLED"
)

// PaymentMethod — способ оплаты заказа
type PaymentMethod string

const (
	PaymentMethodCard          PaymentMethod = "CARD"
	PaymentMethodSBP           PaymentMethod = "SBP"
	PaymentMethodCreditCard    PaymentMethod = "CREDIT_CARD"
	PaymentMethodInvestorMoney PaymentMethod = "INVESTOR_MONEY"
)

// Маппинг PaymentMethod между HTTP и gRPC
var paymentMethodMap = map[orderv1.PaymentMethod]paymentv1.PaymentMethod{
	orderv1.PaymentMethodCARD:          paymentv1.PaymentMethod_PAYMENT_METHOD_CARD,
	orderv1.PaymentMethodSBP:           paymentv1.PaymentMethod_PAYMENT_METHOD_SBP,
	orderv1.PaymentMethodCREDITCARD:    paymentv1.PaymentMethod_PAYMENT_METHOD_CREDIT_CARD,
	orderv1.PaymentMethodINVESTORMONEY: paymentv1.PaymentMethod_PAYMENT_METHOD_INVESTOR_MONEY,
}

var paymentMethodToModelMap = map[orderv1.PaymentMethod]PaymentMethod{
	orderv1.PaymentMethodCARD:          PaymentMethodCard,
	orderv1.PaymentMethodSBP:           PaymentMethodSBP,
	orderv1.PaymentMethodCREDITCARD:    PaymentMethodCreditCard,
	orderv1.PaymentMethodINVESTORMONEY: PaymentMethodInvestorMoney,
}

// Order представляет заказ на постройку космического корабля
type Order struct {
	OrderUUID       uuid.UUID
	HullUUID        uuid.UUID
	EngineUUID      uuid.UUID
	ShieldUUID      *uuid.UUID // опциональный
	WeaponUUID      *uuid.UUID // опциональный
	TotalPrice      int64      // в копейках
	TransactionUUID *uuid.UUID
	PaymentMethod   *PaymentMethod
	Status          OrderStatus
	CreatedAt       time.Time
}

// orderStore — хранилище заказов (in-memory)
type orderStore struct {
	mu     sync.RWMutex
	orders map[uuid.UUID]Order
}

// NewOrderStore создаёт новое пустое хранилище заказов
func NewOrderStore() *orderStore {
	return &orderStore{
		orders: make(map[uuid.UUID]Order),
	}
}

// handler реализует интерфейс orderv1.Handler, сгенерированный ogen
type handler struct {
	orderv1.UnimplementedHandler
	inventoryClient inventoryv1.InventoryServiceClient
	paymentClient   paymentv1.PaymentServiceClient
	store           *orderStore
}

// NewHandler создаёт новый обработчик заказов
func NewHandler(
	inventoryClient inventoryv1.InventoryServiceClient,
	paymentClient paymentv1.PaymentServiceClient,
	store *orderStore,
) *handler {
	return &handler{
		inventoryClient: inventoryClient,
		paymentClient:   paymentClient,
		store:           store,
	}
}

// SetupServer создаёт OpenAPI сервер на основе обработчика
func SetupServer(h *handler) (*orderv1.Server, error) {
	return orderv1.NewServer(h)
}

// GetOrder реализует операцию getOrder (пример реализации)
// GET /api/v1/orders/{order_uuid}.
func (h *handler) GetOrder(_ context.Context, params orderv1.GetOrderParams) (orderv1.GetOrderRes, error) {
	// 1. Найти заказ в store (с блокировкой для thread-safety)
	h.store.mu.RLock()
	order, ok := h.store.orders[params.OrderUUID]
	h.store.mu.RUnlock()

	// 2. Если не найден — вернуть 404
	if !ok {
		return &orderv1.GetOrderNotFound{
			Code:    http.StatusNotFound,
			Message: "заказ не найден",
		}, nil
	}

	// 3. Преобразовать в DTO и вернуть
	return orderToDTO(order), nil
}

// CreateOrder реализует операцию createOrder
// POST /api/v1/orders
func (h *handler) CreateOrder(ctx context.Context, req *orderv1.CreateOrderRequest) (orderv1.CreateOrderRes, error) {
	// 1. Валидация: hull_uuid и engine_uuid обязательны
	if req.GetHullUUID() == uuid.Nil {
		return &orderv1.CreateOrderBadRequest{
			Code:    http.StatusBadRequest,
			Message: "hull_uuid обязателен",
		}, nil
	}

	if req.GetEngineUUID() == uuid.Nil {
		return &orderv1.CreateOrderBadRequest{
			Code:    http.StatusBadRequest,
			Message: "engine_uuid обязателен",
		}, nil
	}

	partUUIDs := []string{
		req.GetEngineUUID().String(),
		req.GetHullUUID().String(),
	}

	// Добавляем не обязательные поля если они есть в массив uuids для передачи в ListParts
	// Если детали shieldUUID и weaponUUID есть, то даем id для создания order
	var shieldUUID *uuid.UUID
	if v, ok := req.ShieldUUID.Get(); ok {
		partUUIDs = append(partUUIDs, v.String())
		shieldUUID = &v
	}

	var weaponUUID *uuid.UUID
	if v, ok := req.WeaponUUID.Get(); ok {
		partUUIDs = append(partUUIDs, v.String())
		weaponUUID = &v
	}

	// 2. Получить детали через InventoryService.ListParts
	grpcCtx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()

	parts, err := h.inventoryClient.ListParts(
		grpcCtx,
		&inventoryv1.ListPartsRequest{
			Uuids: partUUIDs,
		},
	)
	if err != nil {
		return handleGRPCError(ctx, err, "createOrder"), nil
	}

	// 3. Проверить stock_quantity > 0
	// 4. Вычислить total_price
	var totalPrice int64
	for _, part := range parts.Parts {
		if part.StockQuantity <= 0 {
			return &orderv1.CreateOrderConflict{
				Code:    http.StatusConflict,
				Message: fmt.Sprintf("деталь с id:%s отсутствует на складе", part.GetUuid()),
			}, nil
		}
		totalPrice += part.Price
	}

	// 5. Сгенерировать order_uuid (UUID v4)
	orderUUID := uuid.New()

	// 6. Создать заказ со статусом PENDING_PAYMENT
	order := Order{
		OrderUUID:  orderUUID,
		HullUUID:   req.GetHullUUID(),
		EngineUUID: req.GetEngineUUID(),
		ShieldUUID: shieldUUID,
		WeaponUUID: weaponUUID,
		TotalPrice: totalPrice,
		Status:     OrderStatusPendingPayment,
		CreatedAt:  time.Now(),
	}

	slog.InfoContext(ctx, "заказ создан", "order_uuid", orderUUID, "total_price", totalPrice)

	// 7. Сохранить в store
	h.store.mu.Lock()
	h.store.orders[orderUUID] = order
	h.store.mu.Unlock()

	// 8. Вернуть order_uuid и total_price
	return &orderv1.CreateOrderResponse{
		OrderUUID:  orderUUID,
		TotalPrice: totalPrice,
	}, nil
}

// PayOrder реализует операцию payOrder
// POST /api/v1/orders/{order_uuid}/pay
func (h *handler) PayOrder(ctx context.Context, req *orderv1.PayOrderRequest, params orderv1.PayOrderParams) (orderv1.PayOrderRes, error) {
	// 1. Найти заказ в store
	h.store.mu.RLock()
	order, ok := h.store.orders[params.OrderUUID]
	if !ok {
		h.store.mu.RUnlock()
		return &orderv1.PayOrderNotFound{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("order с id %s не найден", params.OrderUUID),
		}, nil
	}

	// 2. Проверить статус == PENDING_PAYMENT
	if order.Status != OrderStatusPendingPayment {
		h.store.mu.RUnlock()
		return &orderv1.PayOrderConflict{
			Code:    http.StatusConflict,
			Message: "заказ не может быть оплачен в текущем статусе: " + string(order.Status),
		}, nil
	}
	h.store.mu.RUnlock()

	// Маппинг HTTP PaymentMethod в gRPC PaymentMethod
	grpcPaymentMethod, ok := paymentMethodMap[req.PaymentMethod]
	if !ok {
		return &orderv1.PayOrderBadRequest{
			Code:    http.StatusBadRequest,
			Message: "неизвестный способ оплаты",
		}, nil
	}

	// Вызвать h.paymentClient.PayOrder для обработки платежа
	grpcCtx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()

	transaction, err := h.paymentClient.PayOrder(grpcCtx, &paymentv1.PayOrderRequest{
		OrderUuid:     order.OrderUUID.String(),
		PaymentMethod: grpcPaymentMethod,
	})
	if err != nil {
		return handlePayGRPCError(ctx, err, "payOrder"), nil
	}

	// Распарсим uuid транзакции
	transactionUUID := uuid.MustParse(transaction.GetTransactionUuid())

	// Обновить статус на PAID, обновить метод оплаты и сохранить transaction_uuid
	h.store.mu.Lock()
	order = h.store.orders[params.OrderUUID]
	order.Status = OrderStatusPaid
	order.TransactionUUID = &transactionUUID
	order.PaymentMethod = new(paymentMethodToModelMap[req.PaymentMethod])
	// делаем запись обновленного order в store
	h.store.orders[params.OrderUUID] = order
	h.store.mu.Unlock()

	slog.InfoContext(ctx, "заказ оплачен", "order_uuid", params.OrderUUID, "transaction_uuid", transactionUUID)

	// Вернуть transaction_uuid
	return &orderv1.PayOrderResponse{
		TransactionUUID: transactionUUID,
	}, nil
}

// CancelOrder реализует операцию cancelOrder
// POST /api/v1/orders/{order_uuid}/cancel
func (h *handler) CancelOrder(ctx context.Context, params orderv1.CancelOrderParams) (orderv1.CancelOrderRes, error) {
	// Найти заказ в store
	h.store.mu.RLock()
	defer h.store.mu.RUnlock()

	order, ok := h.store.orders[params.OrderUUID]

	if !ok {
		return &orderv1.CancelOrderNotFound{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("order с id %s не найден", params.OrderUUID),
		}, nil
	}

	// Проверить статус == PENDING_PAYMENT
	if order.Status != OrderStatusPendingPayment {
		return &orderv1.CancelOrderConflict{
			Code:    http.StatusConflict,
			Message: fmt.Sprintf("нельзя отменить заказ в статусе %s", order.Status),
		}, nil
	}

	// Обновить статус на CANCELLED
	order.Status = OrderStatusCancelled

	h.store.orders[params.OrderUUID] = order

	// Вернуть success
	return &orderv1.CancelOrderResponse{}, nil
}

func orderToDTO(order Order) *orderv1.OrderDto {
	dto := &orderv1.OrderDto{
		OrderUUID:  order.OrderUUID,
		HullUUID:   order.HullUUID,
		EngineUUID: order.EngineUUID,
		TotalPrice: order.TotalPrice,
		Status:     orderv1.OrderStatus(order.Status),
		CreatedAt:  order.CreatedAt,
	}

	if order.ShieldUUID != nil {
		dto.ShieldUUID = orderv1.NewOptNilUUID(*order.ShieldUUID)
	}

	if order.WeaponUUID != nil {
		dto.WeaponUUID = orderv1.NewOptNilUUID(*order.WeaponUUID)
	}

	if order.TransactionUUID != nil {
		dto.TransactionUUID = orderv1.NewOptNilUUID(*order.TransactionUUID)
	}

	if order.PaymentMethod != nil {
		dto.PaymentMethod = orderv1.NewOptNilPaymentMethod(orderv1.PaymentMethod(*order.PaymentMethod))
	}

	return dto
}

func handleGRPCError(ctx context.Context, err error, operation string) orderv1.CreateOrderRes {
	st, ok := status.FromError(err)
	if !ok {
		slog.ErrorContext(ctx, "неизвестная ошибка gRPC", "operation", operation, "error", err)

		return &orderv1.CreateOrderInternalServerError{
			Code:    http.StatusInternalServerError,
			Message: "внутренняя ошибка сервера",
		}
	}

	switch st.Code() {
	case codes.NotFound:
		return &orderv1.CreateOrderNotFound{
			Code:    http.StatusNotFound,
			Message: st.Message(),
		}
	case codes.InvalidArgument:
		return &orderv1.CreateOrderBadRequest{
			Code:    http.StatusBadRequest,
			Message: st.Message(),
		}
	default:
		slog.ErrorContext(ctx, "ошибка gRPC", "operation", operation, "code", st.Code(), "message", st.Message())

		return &orderv1.CreateOrderInternalServerError{
			Code:    http.StatusInternalServerError,
			Message: "внутренняя ошибка сервера",
		}
	}
}

func handlePayGRPCError(ctx context.Context, err error, operation string) orderv1.PayOrderRes {
	st, ok := status.FromError(err)
	if !ok {
		slog.ErrorContext(ctx, "неизвестная ошибка gRPC", "operation", operation, "error", err)

		return &orderv1.PayOrderInternalServerError{
			Code:    http.StatusInternalServerError,
			Message: "внутренняя ошибка сервера",
		}
	}

	switch st.Code() {
	case codes.NotFound:
		return &orderv1.PayOrderNotFound{
			Code:    http.StatusNotFound,
			Message: st.Message(),
		}
	case codes.InvalidArgument:
		return &orderv1.PayOrderBadRequest{
			Code:    http.StatusBadRequest,
			Message: st.Message(),
		}
	case codes.FailedPrecondition, codes.AlreadyExists:
		return &orderv1.PayOrderConflict{
			Code:    http.StatusConflict,
			Message: st.Message(),
		}
	default:
		slog.ErrorContext(ctx, "ошибка gRPC", "operation", operation, "code", st.Code(), "message", st.Message())

		return &orderv1.PayOrderInternalServerError{
			Code:    http.StatusInternalServerError,
			Message: "внутренняя ошибка сервера",
		}
	}
}
