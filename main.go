package main

import (
	"flame_clouds/core"
	"flame_clouds/flags"
	"flame_clouds/global"
	"flame_clouds/service/cron_service"
	"flame_clouds/service/hsy_service"
	"flame_clouds/service/message_push_service"
	"os"
)

func main() {
	// 读取配置文件
	global.Config = core.ReadConfig()

	// 日志
	core.InitLogger()

	// 命令行参数绑定
	flags.Run()

	// 测试推送
	if os.Getenv("TEST_PUSH") == "true" {
		bot := message_push_service.NewMessage(
			global.Config.Bot.TargetList[0].Name,
			global.Config.Bot.TargetList[0].SendKey,
		)

		if bot == nil {
			return
		}

		err := bot.Push(
			"【测试】晚霞监控",
			"这是 Sunset Monitor 的测试消息。如果你能看到这条消息，说明 GitHub Actions → Server酱 → 微信推送链路正常。",
		)

		if err != nil {
			core.InitLogger()
			return
		}

		return
	}

	// 单次运行模式
	if os.Getenv("RUN_ONCE") == "true" {
		hsy_service.GetCitySunsetData(global.Config.Monitor.Evening)
		return
	}

	// 正常模式：启动定时任务
	cron_service.CronService()

	select {}
}
