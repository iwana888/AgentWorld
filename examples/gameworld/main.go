// Command gameworld —— 独立演示：注册一个第三方 Game 世界模块。
//
// 本目录同时提供：
//   - gameworld/gameworld.go：可导入的 SDK 模块（package gameworld），供 AgentWorld 运行时注册。
//   - main.go：独立演示，证明注册链路可用。
//
// 在实际的 AgentWorld 运行时中，第三方世界通过 sdk.RegisterModule 注册后，
// 由运行时 sdk.LoadSDKModules() 自动加载并调度。
package main

import (
	"fmt"

	"agentworld/examples/gameworld/gameworld"
	"agentworld/sdk"
)

func main() {
	sdk.RegisterModule(gameworld.New())
	fmt.Printf("GameWorld 已注册。SDK 已注册模块数=%d\n", sdk.RegisteredCount())
	fmt.Println("提示：在 AgentWorld 运行时中，该世界会被 sdk.LoadSDKModules() 自动加载并调度。")
}
