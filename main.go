package main

import (
	"flame_clouds/core"
	"flame_clouds/flags"
	"flame_clouds/global"
	"flame_clouds/service/cron_service"
	"flame_clouds/service/hsy_service"
	"flame_clouds/service/message_push_service"
	"log"
	"os"
	"time"
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
			log.Printf("测试推送失败: %v", err)
			return
		}

		return
	}

	// 多次检查模式
	if os.Getenv("RUN_MULTI") == "true" {
		for i := 0; i < 4; i++ {
			log.Printf("开始第 %d 次晚霞监控检查", i+1)

			hsy_service.GetCitySunsetData(global.Config.Monitor.Evening)

			if i < 3 {
				log.Printf("本次检查完成，1小时后进行下一次检查")
				time.Sleep(10 * time.Second)
			}
		}

		log.Printf("今日4次晚霞监控全部完成")
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
