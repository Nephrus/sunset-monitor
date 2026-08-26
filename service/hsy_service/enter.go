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
		t, err := GetSunsetData(SunsetBotReq{City: global.Config.Monitor.City, Event: e.EventType.Params(), Aod: e.Quality})
		if err != nil {
			logrus.Errorf("请求错误 %s", err)
			return
		}
		checkAndNotify(t, e)
		return
	}

	for _, city := range global.Config.Monitor.CityList {
		t, err := GetSunsetData(SunsetBotReq{City: city, Event: e.EventType.Params(), Aod: e.Quality})
		if err != nil {
			logrus.Errorf("请求错误 %s", err)
			return
		}
		checkAndNotify(t, e)
	}
}

var qualityRe = regexp.MustCompile(`(\d+\.?\d*)`)

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
	if data.TbQuality == "-" {
		logrus.Infof("城市: %s 当前暂无火烧云质量数据，跳过推送", data.City)
		return
	}

	quality, err := parseQuality(data.TbQuality)
	if err != nil {
		logrus.Errorf("解析火烧云质量失败: %s", err)
		return
	}

	logrus.Infof("城市: %s, 事件: %s, 质量: %.2f", data.City, e.EventType.String(), quality)

	if quality < e.Quality {
		logrus.Warnf("火烧云指标未达到阈值")
		return
	}

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

	if !global.Config.Bot.Enable {
		logrus.Infof("未配置消息推送渠道")
		return
	}

	// 去请求图片数据
	if global.Config.Monitor.Map.Enable {
		response, err1 := GetSunsetMapData(MapReq{
			Region: global.Config.Monitor.Map.Region,
			Event:  e.EventType.Params(),
		})
		if err1 == nil {
			message += fmt.Sprintf("\n![](%s)", "https://sunsetbot.top"+response.MapImgSrc)
		} else {
			logrus.Errorf("请求火烧云地图数据失败 %s", err1)
		}
	}

	title := fmt.Sprintf("[%s] %s预警 质量:%.2f", data.City, e.EventType.String(), quality)

	if global.Config.Bot.Target != "" {
		// 消息推送
		bot := message_push_service.NewMessage(global.Config.Bot.Target, global.Config.Bot.SendKey)
		if bot == nil {
			return
		}
		err = bot.Push(title, message)
		if err != nil {
			logrus.Errorf("消息推送失败 %s", err)
		}
		return
	}
	for _, target := range global.Config.Bot.TargetList {
		// 消息推送
		bot := message_push_service.NewMessage(target.Name, target.SendKey)
		if bot == nil {
			return
		}
		err = bot.Push(title, message)
		if err != nil {
			logrus.Errorf("消息推送失败 %s", err)
		}
	}
}
