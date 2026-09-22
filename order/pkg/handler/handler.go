package handler

import (
	"context"
	"fmt"
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
func mapPaymentMethod(httpPaymentMethod orderv1.PaymentMethod) paymentv1.PaymentMethod {
	switch httpPaymentMethod {
	case orderv1.PaymentMethodCARD:
		return paymentv1.PaymentMethod_PAYMENT_METHOD_CARD
	case orderv1.PaymentMethodSBP:
		return paymentv1.PaymentMethod_PAYMENT_METHOD_SBP
	case orderv1.PaymentMethodCREDITCARD:
		return paymentv1.PaymentMethod_PAYMENT_METHOD_CREDIT_CARD
	case orderv1.PaymentMethodINVESTORMONEY:
		return paymentv1.PaymentMethod_PAYMENT_METHOD_INVESTOR_MONEY
	default:
		return paymentv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED
	}
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
	var shieldUUID orderv1.OptNilUUID
	if order.ShieldUUID != nil {
		shieldUUID = orderv1.NewOptNilUUID(*order.ShieldUUID)
	}

	var weaponUUID orderv1.OptNilUUID
	if order.WeaponUUID != nil {
		weaponUUID = orderv1.NewOptNilUUID(*order.WeaponUUID)
	}

	var transactionUUID orderv1.OptNilUUID
	if order.TransactionUUID != nil {
		transactionUUID = orderv1.NewOptNilUUID(*order.TransactionUUID)
	}

	var paymentMethod orderv1.OptNilPaymentMethod
	if order.PaymentMethod != nil {
		paymentMethod = orderv1.NewOptNilPaymentMethod(orderv1.PaymentMethod(*order.PaymentMethod))
	}

	return &orderv1.OrderDto{
		OrderUUID:       order.OrderUUID,
		HullUUID:        order.HullUUID,
		EngineUUID:      order.EngineUUID,
		ShieldUUID:      shieldUUID,
		WeaponUUID:      weaponUUID,
		TotalPrice:      order.TotalPrice,
		TransactionUUID: transactionUUID,
		PaymentMethod:   paymentMethod,
		Status:          orderv1.OrderStatus(order.Status),
		CreatedAt:       order.CreatedAt,
	}, nil
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
	var weaponUUID *uuid.UUID
	if req.ShieldUUID.Set && !req.ShieldUUID.Null {
		partUUIDs = append(partUUIDs, req.ShieldUUID.Value.String())
		shieldUUID = &req.ShieldUUID.Value
	}

	if req.WeaponUUID.Set && !req.WeaponUUID.Null {
		partUUIDs = append(partUUIDs, req.WeaponUUID.Value.String())
		weaponUUID = &req.WeaponUUID.Value
	}

	// 2. Получить детали через InventoryService.ListParts
	parts, err := h.inventoryClient.ListParts(
		ctx,
		&inventoryv1.ListPartsRequest{
			Uuids: partUUIDs,
		},
	)
	if err != nil {
		st, ok := status.FromError(err)
		if !ok {
			return &orderv1.CreateOrderInternalServerError{
				Code:    http.StatusInternalServerError,
				Message: "ошибка сервера",
			}, nil
		}

		switch st.Code() {
		case codes.NotFound:
			return &orderv1.CreateOrderNotFound{
				Code:    http.StatusNotFound,
				Message: st.Message(),
			}, nil
		case codes.InvalidArgument:
			return &orderv1.CreateOrderBadRequest{
				Code:    http.StatusBadRequest,
				Message: st.Message(),
			}, nil
		default:
			return &orderv1.CreateOrderInternalServerError{
				Code:    http.StatusInternalServerError,
				Message: "ошибка сервера",
			}, nil
		}
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
	h.store.mu.RUnlock()

	if !ok {
		return &orderv1.PayOrderNotFound{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("order с id %s не найден", params.OrderUUID),
		}, nil
	}

	// 2. Проверить статус == PENDING_PAYMENT
	if order.Status != OrderStatusPendingPayment {
		return &orderv1.PayOrderConflict{
			Code:    http.StatusConflict,
			Message: "не верный статус платежа",
		}, nil
	}
	// Вызвать h.paymentClient.PayOrder для обработки платежа
	transaction, err := h.paymentClient.PayOrder(ctx, &paymentv1.PayOrderRequest{
		OrderUuid:     order.OrderUUID.String(),
		PaymentMethod: mapPaymentMethod(req.GetPaymentMethod()),
	})
	if err != nil {
		return &orderv1.PayOrderInternalServerError{
			Code:    http.StatusInternalServerError,
			Message: "ошибка платежа",
		}, nil
	}

	// Распарсим uuid транзакции
	transactionUUID, err := uuid.Parse(transaction.GetTransactionUuid())
	if err != nil {
		return &orderv1.PayOrderInternalServerError{
			Code:    http.StatusInternalServerError,
			Message: "некорректный transaction_uuid от PaymentService",
		}, nil
	}

	// Обновить статус на PAID, обновить метод оплаты и сохранить transaction_uuid
	order.Status = OrderStatusPaid
	order.TransactionUUID = &transactionUUID

	paymentMethod := PaymentMethod(req.GetPaymentMethod())
	order.PaymentMethod = &paymentMethod

	// делаем запись обновленного order в store
	h.store.mu.Lock()
	h.store.orders[params.OrderUUID] = order
	h.store.mu.Unlock()

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
	order, ok := h.store.orders[params.OrderUUID]
	h.store.mu.RUnlock()

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

	h.store.mu.Lock()
	h.store.orders[params.OrderUUID] = order
	h.store.mu.Unlock()

	// Вернуть success
	return &orderv1.CancelOrderResponse{}, nil
}
