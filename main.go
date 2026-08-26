package main

import (
	"flame_clouds/core"
	"flame_clouds/flags"
	"flame_clouds/global"
	"flame_clouds/service/cron_service"
	"flame_clouds/service/hsy_service"
	"os"
)

func main() {
	// 读取配置文件
	global.Config = core.ReadConfig()

	// 日志
	core.InitLogger()

	// 命令行参数绑定
	flags.Run()

	// 如果设置 RUN_ONCE=true，则只执行一次晚霞检查
	if os.Getenv("RUN_ONCE") == "true" {
		hsy_service.GetCitySunsetData(global.Config.Monitor.Evening)
		return
	}

	// 正常模式：启动定时任务
	cron_service.CronService()

	select {}
}
