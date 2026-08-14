// main.go —— Hotel World 入口（M8.1 Hotel Spatial World）。
//
// 构建一个标准酒店空间（Hotel 001），启动 HTTP 观测服务（默认 :19200）。
//
// 布局（需求三）：
//
//	Hotel 001
//	├── Entrance
//	├── Lobby
//	│   ├── WelcomeArea
//	│   └── WaitingArea
//	├── FrontDesk (Desk01/Desk02)
//	├── Restaurant
//	├── Elevator
//	├── Floor01 (Room101/102/103)
//	└── Floor02 (Room201/202)
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	hh "agentworld/worlds/hotel/hotel"
)

func main() {
	addr := ":19200"
	if v := os.Getenv("HOTEL_OBS_ADDR"); v != "" {
		addr = v
	}
	flag.Parse()

	world := buildHotel()
	srv := hh.NewServer(world)

	log.Printf("[hotel] Hotel World 启动：%s (%s)，监听 %s", world.Space().Name(), world.Space().HotelID(), addr)
	http.Handle("/", srv.Mux())
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("[hotel] serve: %v", err)
	}
}

// buildHotel 构建 Hotel 001 的空间世界 + 员工 + 测试 Guest。
func buildHotel() *hh.SpaceWorld {
	w := hh.NewSpaceWorld("hotel001", "Hotel 001", "一座用于空间感知实验的虚拟酒店")

	space := w.Space()
	// ---- 位置（二维坐标）----
	space.AddLocation(&hh.Location{ID: "entrance", Name: "Entrance", Type: "entrance", X: 0, Y: 0})
	space.AddLocation(&hh.Location{ID: "lobby", Name: "Lobby", Type: "lobby", X: 0, Y: 2})
	space.AddLocation(&hh.Location{ID: "welcome_area", Name: "WelcomeArea", Type: "area", X: -1, Y: 2.5})
	space.AddLocation(&hh.Location{ID: "waiting_area", Name: "WaitingArea", Type: "area", X: 1, Y: 2.5})
	space.AddLocation(&hh.Location{ID: "frontdesk", Name: "FrontDesk", Type: "frontdesk", X: 2, Y: 2})
	space.AddLocation(&hh.Location{ID: "restaurant", Name: "Restaurant", Type: "restaurant", X: -2, Y: 3})
	space.AddLocation(&hh.Location{ID: "elevator", Name: "Elevator", Type: "elevator", X: 0, Y: 4})
	space.AddLocation(&hh.Location{ID: "floor01", Name: "Floor01", Type: "floor", X: 1, Y: 5})
	space.AddLocation(&hh.Location{ID: "room101", Name: "Room101", Type: "room", X: 0, Y: 6})
	space.AddLocation(&hh.Location{ID: "room102", Name: "Room102", Type: "room", X: 1, Y: 6})
	space.AddLocation(&hh.Location{ID: "floor02", Name: "Floor02", Type: "floor", X: 2, Y: 5})
	space.AddLocation(&hh.Location{ID: "room201", Name: "Room201", Type: "room", X: 2, Y: 6})
	space.AddLocation(&hh.Location{ID: "room202", Name: "Room202", Type: "room", X: 3, Y: 6})

	// ---- 连接（双向）----
	space.Connect("entrance", "lobby")
	space.Connect("lobby", "welcome_area")
	space.Connect("lobby", "waiting_area")
	space.Connect("lobby", "frontdesk")
	space.Connect("lobby", "restaurant")
	space.Connect("lobby", "elevator")
	space.Connect("elevator", "floor01")
	space.Connect("elevator", "floor02")
	space.Connect("floor01", "room101")
	space.Connect("floor01", "room102")
	space.Connect("floor02", "room201")
	space.Connect("floor02", "room202")

	// ---- 酒店员工（含岗位 + 责任区域 + 优先级）----
	w.AddAgent(&hh.Agent{ID: 1, Kind: "ai", Name: "Alice", Role: "welcome", HotelID: "hotel001", Location: "entrance"})
	w.SetAgentRole(1, "welcome", "entrance", 100)
	w.AddAgent(&hh.Agent{ID: 2, Kind: "ai", Name: "Tom", Role: "welcome", HotelID: "hotel001", Location: "entrance"})
	w.SetAgentRole(2, "welcome", "entrance", 80)
	w.AddAgent(&hh.Agent{ID: 3, Kind: "ai", Name: "Bob", Role: "frontdesk", HotelID: "hotel001", Location: "frontdesk"})
	w.SetAgentRole(3, "frontdesk", "frontdesk", 100)

	return w
}
