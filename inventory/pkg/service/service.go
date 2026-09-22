package service

import (
	"cmp"
	"context"
	"slices"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	inventoryv1 "github.com/Andrew1996-la/stellar-works/shared/pkg/proto/inventory/v1"
)

// Part представляет деталь космического корабля
type Part struct {
	UUID          string
	Name          string
	Description   string
	Price         int64 // в копейках
	PartType      inventoryv1.PartType
	StockQuantity int64
	CreatedAt     *timestamppb.Timestamp
}

// server реализует gRPC сервис
type server struct {
	inventoryv1.UnimplementedInventoryServiceServer
	parts map[uuid.UUID]Part
}

// NewServer создаёт сервер с предзагруженными seed-данными
func NewServer() *server {
	now := timestamppb.Now()

	return &server{
		parts: map[uuid.UUID]Part{
			uuid.MustParse("550e8400-e29b-41d4-a716-446655440001"): {
				UUID:          "550e8400-e29b-41d4-a716-446655440001",
				Name:          "Алюминиевый корпус",
				Description:   "Лёгкий корпус для небольших кораблей",
				Price:         500000, // 5000₽
				PartType:      inventoryv1.PartType_PART_TYPE_HULL,
				StockQuantity: 10,
				CreatedAt:     now,
			},
			uuid.MustParse("550e8400-e29b-41d4-a716-446655440002"): {
				UUID:          "550e8400-e29b-41d4-a716-446655440002",
				Name:          "Титановый корпус",
				Description:   "Прочный корпус для средних кораблей",
				Price:         1500000, // 15000₽
				PartType:      inventoryv1.PartType_PART_TYPE_HULL,
				StockQuantity: 5,
				CreatedAt:     now,
			},
			uuid.MustParse("550e8400-e29b-41d4-a716-446655440003"): {
				UUID:          "550e8400-e29b-41d4-a716-446655440003",
				Name:          "Ионный двигатель C",
				Description:   "Базовый ионный двигатель класса C",
				Price:         300000, // 3000₽
				PartType:      inventoryv1.PartType_PART_TYPE_ENGINE,
				StockQuantity: 8,
				CreatedAt:     now,
			},
			uuid.MustParse("550e8400-e29b-41d4-a716-446655440004"): {
				UUID:          "550e8400-e29b-41d4-a716-446655440004",
				Name:          "Ионный двигатель B",
				Description:   "Улучшенный ионный двигатель класса B",
				Price:         800000, // 8000₽
				PartType:      inventoryv1.PartType_PART_TYPE_ENGINE,
				StockQuantity: 3,
				CreatedAt:     now,
			},
			uuid.MustParse("550e8400-e29b-41d4-a716-446655440005"): {
				UUID:          "550e8400-e29b-41d4-a716-446655440005",
				Name:          "Энергетический щит",
				Description:   "Стандартный энергетический щит",
				Price:         400000, // 4000₽
				PartType:      inventoryv1.PartType_PART_TYPE_SHIELD,
				StockQuantity: 6,
				CreatedAt:     now,
			},
			uuid.MustParse("550e8400-e29b-41d4-a716-446655440006"): {
				UUID:          "550e8400-e29b-41d4-a716-446655440006",
				Name:          "Лазерная пушка",
				Description:   "Точная лазерная пушка",
				Price:         250000, // 2500₽
				PartType:      inventoryv1.PartType_PART_TYPE_WEAPON,
				StockQuantity: 7,
				CreatedAt:     now,
			},
			uuid.MustParse("550e8400-e29b-41d4-a716-446655440007"): {
				UUID:          "550e8400-e29b-41d4-a716-446655440007",
				Name:          "Плазменный корпус",
				Description:   "Экспериментальный корпус (нет на складе)",
				Price:         2000000, // 20000₽
				PartType:      inventoryv1.PartType_PART_TYPE_HULL,
				StockQuantity: 0,
				CreatedAt:     now,
			},
		},
	}
}

// GetPart возвращает деталь по UUID
func (s *server) GetPart(
	ctx context.Context,
	req *inventoryv1.GetPartRequest,
) (*inventoryv1.GetPartResponse, error) {
	// Проверить, что uuid не пустой → INVALID_ARGUMENT
	if req.GetUuid() == "" {
		return nil, status.Error(codes.InvalidArgument, "uuid обязателен")
	}
	// Валидировать формат UUID → INVALID_ARGUMENT
	id, err := uuid.Parse(req.GetUuid())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "неверный формат uuid: %s", req.GetUuid())
	}

	// Найти деталь в map
	// Если не найдена → NOT_FOUND
	part, ok := s.parts[id]
	if !ok {
		return nil, status.Error(codes.NotFound, "деталь не найдена")
	}

	// Преобразовать в inventoryv1.Part
	grpcPart := &inventoryv1.Part{
		Uuid:          part.UUID,
		Name:          part.Name,
		Description:   part.Description,
		Price:         part.Price,
		PartType:      part.PartType,
		StockQuantity: part.StockQuantity,
		CreatedAt:     part.CreatedAt,
	}

	// Вернуть деталь
	return &inventoryv1.GetPartResponse{
		Part: grpcPart,
	}, nil
}

// ListParts возвращает список деталей с опциональной фильтрацией по типу
func (s *server) ListParts(
	ctx context.Context,
	req *inventoryv1.ListPartsRequest,
) (*inventoryv1.ListPartsResponse, error) {
	//  Если передан список uuids → найти детали по UUID (сохраняя порядок запроса)
	//    - Проверить формат каждого UUID → INVALID_ARGUMENT
	//    - Если хоть один UUID не найден → NOT_FOUND
	if len(req.Uuids) > 0 {
		partsListGrpc := make([]*inventoryv1.Part, len(req.Uuids))

		for index, partUuid := range req.Uuids {
			id, err := uuid.Parse(partUuid)
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "неверный формат uuid: %s", partUuid)
			}

			part, ok := s.parts[id]
			if !ok {
				return nil, status.Errorf(codes.NotFound, "деталь c uuid %s не найдена", id)
			}

			partsListGrpc[index] = &inventoryv1.Part{
				Uuid:          part.UUID,
				Name:          part.Name,
				Description:   part.Description,
				Price:         part.Price,
				PartType:      part.PartType,
				StockQuantity: part.StockQuantity,
				CreatedAt:     part.CreatedAt,
			}
		}

		return &inventoryv1.ListPartsResponse{
			Parts: partsListGrpc,
		}, nil
	}

	// иначе если part_type == UNSPECIFIED → вернуть все детали
	partsListGrpc := make([]*inventoryv1.Part, 0)
	for _, part := range s.parts {
		// проверяю что деталь не UNSPECIFIED и что она нужного типа
		if req.GetPartType() != inventoryv1.PartType_PART_TYPE_UNSPECIFIED && part.PartType != req.PartType {
			continue
		}

		grpcPart := &inventoryv1.Part{
			Uuid:          part.UUID,
			Name:          part.Name,
			Description:   part.Description,
			Price:         part.Price,
			PartType:      part.PartType,
			StockQuantity: part.StockQuantity,
			CreatedAt:     part.CreatedAt,
		}

		partsListGrpc = append(partsListGrpc, grpcPart)
	}

	// Иначе → фильтровать по типу
	// Отсортировать по имени (только для фильтрации по типу, не для uuids)
	slices.SortFunc(partsListGrpc, func(a, b *inventoryv1.Part) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return &inventoryv1.ListPartsResponse{
		Parts: partsListGrpc,
	}, nil
}
