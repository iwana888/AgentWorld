// hotel.go — 酒店世界（HotelModule）的数据访问层。
// 只提供酒店专属的查询/写入，不涉及社交世界的表。
package db

import (
	"agentworld/internal/models"

	"gorm.io/gorm"
)

// HotelRoom 状态常量
const (
	RoomAvailable   = "available"
	RoomOccupied    = "occupied"
	RoomCleaning    = "cleaning"
	RoomMaintenance = "maintenance"
)

// ListRooms 按状态查房间；status 为空查全部。
func ListRooms(d *gorm.DB, status string) ([]models.HotelRoom, error) {
	var rooms []models.HotelRoom
	q := d.Model(&models.HotelRoom{}).Order("floor ASC, number ASC")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Find(&rooms).Error
	return rooms, err
}

// CountRoomsByStatus 返回各状态房间数（available/occupied/cleaning/maintenance）。
func CountRoomsByStatus(d *gorm.DB) (map[string]int64, error) {
	m := map[string]int64{}
	var rows []struct {
		Status string
		Cnt    int64
	}
	err := d.Model(&models.HotelRoom{}).Select("status, count(*) as cnt").Group("status").Scan(&rows).Error
	for _, r := range rows {
		m[r.Status] = r.Cnt
	}
	return m, err
}

// RoomByID 查单个房间。
func RoomByID(d *gorm.DB, id int64) (models.HotelRoom, error) {
	var r models.HotelRoom
	err := d.First(&r, id).Error
	return r, err
}

// SetRoomStatus 修改房间状态并返回旧状态。
func SetRoomStatus(d *gorm.DB, id int64, status string) (string, error) {
	var r models.HotelRoom
	if err := d.First(&r, id).Error; err != nil {
		return "", err
	}
	old := r.Status
	if err := d.Model(&models.HotelRoom{}).Where("id = ?", id).Update("status", status).Error; err != nil {
		return old, err
	}
	return old, nil
}

// InsertBooking 写入一条预订。
func InsertBooking(d *gorm.DB, b *models.HotelBooking) error {
	return d.Create(b).Error
}

// ActiveBookingByAgent 查某 Agent 当前未退房的预订。
func ActiveBookingByAgent(d *gorm.DB, agentID int64) (models.HotelBooking, error) {
	var b models.HotelBooking
	err := d.Where("agent_id = ? AND status = ?", agentID, "active").First(&b).Error
	return b, err
}

// CheckoutBooking 退房：更新预订状态为 checked_out。
func CheckoutBooking(d *gorm.DB, id int64) error {
	return d.Model(&models.HotelBooking{}).Where("id = ?", id).Update("status", "checked_out").Error
}

// CountActiveBookings 活跃预订数。
func CountActiveBookings(d *gorm.DB) (int64, error) {
	var n int64
	err := d.Model(&models.HotelBooking{}).Where("status = ?", "active").Count(&n).Error
	return n, err
}

// InsertReview 写入一条评价。
func InsertReview(d *gorm.DB, r *models.HotelReview) error {
	return d.Create(r).Error
}

// ReviewCountByScore 按分数统计评价数。
func ReviewCountByScore(d *gorm.DB) (map[int64]int64, error) {
	m := map[int64]int64{}
	var rows []struct {
		Score int64
		Cnt   int64
	}
	err := d.Model(&models.HotelReview{}).Select("score, count(*) as cnt").Group("score").Scan(&rows).Error
	for _, r := range rows {
		m[r.Score] = r.Cnt
	}
	return m, err
}

// HotelRoomsRow 房间行（含状态文本）。
type HotelRoomsRow struct {
	ID      int64
	Number  string
	RoomType string
	Price   int
	Status  string
	Floor   int
}

// SeedHotelRooms 初始化默认房间（幂等：已存在则不重建）。
func SeedHotelRooms(d *gorm.DB) (int, error) {
	var count int64
	d.Model(&models.HotelRoom{}).Count(&count)
	if count > 0 {
		return int(count), nil
	}
	seeds := []models.HotelRoom{
		{Number: "101", RoomType: "standard", Price: 399, Floor: 1, Status: RoomAvailable},
		{Number: "102", RoomType: "standard", Price: 399, Floor: 1, Status: RoomAvailable},
		{Number: "103", RoomType: "standard", Price: 429, Floor: 1, Status: RoomAvailable},
		{Number: "201", RoomType: "deluxe", Price: 699, Floor: 2, Status: RoomAvailable},
		{Number: "202", RoomType: "deluxe", Price: 699, Floor: 2, Status: RoomAvailable},
		{Number: "203", RoomType: "deluxe", Price: 729, Floor: 2, Status: RoomAvailable},
		{Number: "301", RoomType: "suite", Price: 1299, Floor: 3, Status: RoomAvailable},
		{Number: "302", RoomType: "suite", Price: 1299, Floor: 3, Status: RoomAvailable},
	}
	if err := d.Create(&seeds).Error; err != nil {
		return 0, err
	}
	return len(seeds), nil
}
