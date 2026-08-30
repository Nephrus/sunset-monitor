package hsy_service

import (
	"encoding/json"
	"flame_clouds/config"
	"flame_clouds/global"
	"flame_clouds/service/message_push_service"
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SunsetBotReq struct {
	City  string  `json:"city"`
	Aod   float64 `json:"aod"`
	Event string  `json:"event"` // set_1:今天日落, rise_2:明天日出
}

type SunsetBotResponse struct {
	ImgHref     string `json:"img_href"`
	ImgSummary  string `json:"img_summary"`
	PlaceHolder string `json:"place_holder"`
	QueryId     string `json:"query_id"`
	Status      string `json:"status"`
	TbAod       string `json:"tb_aod"`
	TbEventTime string `json:"tb_event_time"` // 事件时间
	TbQuality   string `json:"tb_quality"`    // 火烧云指标

	City string `json:"city"` // 用于参数传递
}

// 生成随机查询ID
func generateQueryID() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return strconv.Itoa(r.Intn(10000000) + 1)
}

// GetSunsetData 获取日落/日出数据
func GetSunsetData(req SunsetBotReq) (*SunsetBotResponse, error) {
	queryID := generateQueryID()
	baseURL := "https://sunsetbot.top/"

	params := url.Values{}
	params.Add("query_id", queryID)
	params.Add("intend", "select_city")
	params.Add("query_city", req.City)
	params.Add("event_date", "None")
	params.Add("event", req.Event)
	params.Add("times", "None")

	fullURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	var lastErr error

	for attempt := 1; attempt <= 3; attempt++ {
		resp, err := http.Get(fullURL)

		if err != nil {
			lastErr = fmt.Errorf("请求失败: %w", err)
		} else {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()

			if readErr != nil {
				lastErr = fmt.Errorf("读取响应失败: %w", readErr)
			} else if resp.StatusCode != http.StatusOK {
				lastErr = fmt.Errorf("API返回非200状态码: %d", resp.StatusCode)
			} else {
				var data SunsetBotResponse

				if err := json.Unmarshal(body, &data); err != nil {
					lastErr = fmt.Errorf("JSON解析失败: %w", err)
				} else {
					data.City = req.City
					return &data, nil
				}
			}
		}

		if attempt < 3 {
			logrus.Warnf(
				"请求%s失败，第%d次尝试，2秒后重试: %s",
				req.City,
				attempt,
				lastErr,
			)
			time.Sleep(2 * time.Second)
		}
	}

	return nil, lastErr
}

// GetCitySunsetData 获取指定城市的天气数据
func GetCitySunsetData(e config.MonitorEvent) {
	if global.Config.Monitor.City != "" {
		t, err := GetSunsetData(SunsetBotReq{
			City:  global.Config.Monitor.City,
			Event: e.EventType.Params(),
			Aod:   e.Quality,
		})

		if err != nil {
			logrus.Errorf("请求错误 %s", err)
			return
		}

		checkAndNotify(t, e)
		return
	}

	for _, city := range global.Config.Monitor.CityList {
		t, err := GetSunsetData(SunsetBotReq{
			City:  city,
			Event: e.EventType.Params(),
			Aod:   e.Quality,
		})

		if err != nil {
			logrus.Errorf("请求错误 %s", err)
			continue
		}

		checkAndNotify(t, e)
	}
}

var qualityRe = regexp.MustCompile(`(\d+\.?\d*)`)

// 已经成功推送过的城市
// key: 城市
// value: 日期，例如 2026-08-30
var notifiedCities = make(map[string]string)

// 防止多次检查时同时修改 notifiedCities
var notifiedMutex sync.Mutex

// 解析火烧云指标
func parseQuality(qualityStr string) (float64, error) {
	// 使用正则表达式提取数字部分
	match := qualityRe.FindStringSubmatch(qualityStr)

	if len(match) > 0 {
		return strconv.ParseFloat(match[0], 64)
	}

	return 0, fmt.Errorf("解析失败 %s", qualityStr)
}

// 检查并处理火烧云指标
func checkAndNotify(data *SunsetBotResponse, e config.MonitorEvent) {
	// 网站暂时没有返回火烧云数据
	if data.TbQuality == "-" {
		logrus.Infof(
			"城市: %s 当前暂无火烧云质量数据，跳过推送",
			data.City,
		)
		return
	}

	quality, err := parseQuality(data.TbQuality)
	if err != nil {
		logrus.Errorf("解析火烧云质量失败: %s", err)
		return
	}

	logrus.Infof(
		"城市: %s, 事件: %s, 质量: %.2f",
		data.City,
		e.EventType.String(),
		quality,
	)

	// 没有达到推送阈值
	if quality < e.Quality {
		logrus.Warnf(
			"城市: %s 火烧云指标 %.2f 未达到阈值 %.2f",
			data.City,
			quality,
			e.Quality,
		)
		return
	}

	// 检查今天是否已经成功推送过
	today := time.Now().Format("2006-01-02")

	notifiedMutex.Lock()

	if notifiedCities[data.City] == today {
		notifiedMutex.Unlock()

		logrus.Infof(
			"城市: %s 今天已经推送过，跳过重复推送",
			data.City,
		)
		return
	}

	notifiedMutex.Unlock()

	// 构建消息内容
	message := fmt.Sprintf(
		"【火烧云预警】城市: %s  事件: %s  时间: %s  火烧云质量: %.2f 满足拍摄条件!",
		data.City,
		e.EventType.String(),
		data.TbEventTime,
		quality,
	)

	message = strings.ReplaceAll(message, "<br>", "")

	logrus.Infof(message)

	// 未启用机器人
	if !global.Config.Bot.Enable {
		logrus.Infof("未配置消息推送渠道")
		return
	}

	// 请求火烧云地图
	if global.Config.Monitor.Map.Enable {
		response, err1 := GetSunsetMapData(MapReq{
			Region: global.Config.Monitor.Map.Region,
			Event:  e.EventType.Params(),
		})

		if err1 == nil {
			message += fmt.Sprintf(
				"\n![](%s)",
				"https://sunsetbot.top"+response.MapImgSrc,
			)
		} else {
			logrus.Errorf(
				"请求火烧云地图数据失败 %s",
				err1,
			)
		}
	}

	title := fmt.Sprintf(
		"[%s] %s预警 质量:%.2f",
		data.City,
		e.EventType.String(),
		quality,
	)

	// =========================
	// 单个推送目标
	// =========================
	if global.Config.Bot.Target != "" {
		bot := message_push_service.NewMessage(
			global.Config.Bot.Target,
			global.Config.Bot.SendKey,
		)

		if bot == nil {
			return
		}

		err = bot.Push(title, message)

		if err != nil {
			logrus.Errorf(
				"消息推送失败 %s",
				err,
			)
			return
		}

		// 只有推送成功后才记录
		notifiedMutex.Lock()
		notifiedCities[data.City] = today
		notifiedMutex.Unlock()

		logrus.Infof(
			"城市: %s 今日推送成功，后续检查将不再重复推送",
			data.City,
		)

		return
	}

	// =========================
	// 多个推送目标
	// =========================
	allSuccess := true

	for _, target := range global.Config.Bot.TargetList {
		bot := message_push_service.NewMessage(
			target.Name,
			target.SendKey,
		)

		if bot == nil {
			allSuccess = false
			continue
		}

		err = bot.Push(title, message)

		if err != nil {
			logrus.Errorf(
				"消息推送失败 %s",
				err,
			)
			allSuccess = false
		}
	}

	// 所有推送目标都成功后，才记录为当天已经推送
	if allSuccess {
		notifiedMutex.Lock()
		notifiedCities[data.City] = today
		notifiedMutex.Unlock()

		logrus.Infof(
			"城市: %s 今日推送成功，后续检查将不再重复推送",
			data.City,
		)
	} else {
		logrus.Warnf(
			"城市: %s 部分消息推送失败，不记录为已推送",
			data.City,
		)
	}
}
