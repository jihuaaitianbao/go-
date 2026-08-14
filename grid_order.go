package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

// GridOrderData 现货网格下单结果
type GridOrderData struct {
	Status        string
	CouponID      string
	StrategyID    string
	Profit        string
	Detail        string
	OperationTime string
	IP            string
}

// newHTTPClient 创建带连接池的HTTP客户端，复用TCP+TLS连接
func newHTTPClient(proxy string) *http.Client {
	transport, _ := createProxyTransport(proxy)
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}
}

// GetCouponID 获取优惠券ID
func GetCouponID(proToken, kycToken, ucToken, ua, fingerprint, vtoken, uid string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/hbg/v1/open/voucher/user/list?types=18,19&state=0"

	client := newHTTPClient(proxy)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-api-version", "1.7")
	req.Header.Set("hb-kyc-token", kycToken)
	req.Header.Set("hb-uc-token", ucToken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-client-platform", "android")
	req.Header.Set("huobi-app-version", "11.29.0")
	req.Header.Set("huobi-app-version-code", "112900")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "112900")
	req.Header.Set("user-agent", "BigHuobi/11.29.0 (Android 12; SM-F926U) Build/192 hbApp")
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("hb-uc-ua", ua)
	req.Header.Set("huobi-risefall-kline-utc", "8")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-website", "huobi.pro")
	req.Header.Set("homepagegray", "1")
	req.Header.Set("huobi-app-channel", "7890747")
	req.Header.Set("hb-country-id", "37")
	req.Header.Set("hb-region-id", "41")
	req.Header.Set("hb-ctx-id", uid)
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 处理gzip压缩响应
	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("解压gzip失败: %v", err)
		}
		defer reader.Close()
	default:
		reader = resp.Body
	}

	body, _ := io.ReadAll(reader)
	log.Printf("[GetCouponID] 响应状态码: %d, 响应内容: %s", resp.StatusCode, string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析JSON失败: %v, 响应: %s", err, string(body))
	}

	// 检查错误码
	if code, ok := result["code"].(float64); ok && code != 200 {
		return "", fmt.Errorf("接口返回错误码: %v, 消息: %v", code, result["message"])
	}

	// 尝试多种路径解析优惠券
	if data, ok := result["data"].(map[string]interface{}); ok {
		// 方式1: data.couponList[0].id
		if couponList, ok := data["couponList"].([]interface{}); ok && len(couponList) > 0 {
			if coupon, ok := couponList[0].(map[string]interface{}); ok {
				if id, ok := coupon["id"].(float64); ok {
					log.Printf("[GetCouponID] 找到优惠券ID: %d", int64(id))
					return fmt.Sprintf("%d", int64(id)), nil
				}
				if id, ok := coupon["id"].(string); ok {
					log.Printf("[GetCouponID] 找到优惠券ID: %s", id)
					return id, nil
				}
			}
		}
		// 方式2: data.list[0].id
		if list, ok := data["list"].([]interface{}); ok && len(list) > 0 {
			if item, ok := list[0].(map[string]interface{}); ok {
				if id, ok := item["id"].(float64); ok {
					log.Printf("[GetCouponID] 找到优惠券ID(list): %d", int64(id))
					return fmt.Sprintf("%d", int64(id)), nil
				}
				if id, ok := item["id"].(string); ok {
					log.Printf("[GetCouponID] 找到优惠券ID(list): %s", id)
					return id, nil
				}
			}
		}
	}

	// 方式3: data 直接是列表
	if list, ok := result["data"].([]interface{}); ok && len(list) > 0 {
		if item, ok := list[0].(map[string]interface{}); ok {
			if id, ok := item["id"].(float64); ok {
				log.Printf("[GetCouponID] 找到优惠券ID(data数组): %d", int64(id))
				return fmt.Sprintf("%d", int64(id)), nil
			}
			if id, ok := item["id"].(string); ok {
				log.Printf("[GetCouponID] 找到优惠券ID(data数组): %s", id)
				return id, nil
			}
		}
	}

	return "", fmt.Errorf("未找到优惠券，响应: %s", string(body))
}

// GridOrderRequest 现货网格下单请求体
type GridOrderRequest struct {
	Symbol            string   `json:"symbol"`
	CopyID            int      `json:"copyId"`
	BTCopyID          int      `json:"btCopyId"`
	Source            string   `json:"source"`
	InvestType        int      `json:"investType"`
	RunType           int      `json:"runType"`
	GridNum           int      `json:"gridNum"`
	MinPrice          string   `json:"minPrice"`
	MaxPrice          string   `json:"maxPrice"`
	Coupons           []Coupon `json:"coupons"`
	CouponAmount      string   `json:"couponAmount"`
	RealQuoteAmount   string   `json:"realQuoteAmount"`
	InvestQuoteAmount string   `json:"investQuoteAmount"`
}

// Coupon 优惠券
type Coupon struct {
	ID     string `json:"id"`
	Amount string `json:"amount"`
}

// PlaceGridOrder 下单现货网格
func PlaceGridOrder(proToken, kycToken, ucToken, ua, fingerprint, vtoken, uid, couponID string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/hbg/v1/quantization/gridding/strategy-commit"

	client := newHTTPClient(proxy)

	orderReq := GridOrderRequest{
		Symbol:            "trxusdt",
		CopyID:            0,
		BTCopyID:          0,
		Source:            "spot-android",
		InvestType:        2,
		RunType:           0,
		GridNum:           5,
		MinPrice:          "0.170000",
		MaxPrice:          "0.500000",
		Coupons:           []Coupon{{ID: couponID, Amount: "5"}},
		CouponAmount:      "5",
		RealQuoteAmount:   "0.000000",
		InvestQuoteAmount: "5.000000",
	}

	jsonBody, _ := json.Marshal(orderReq)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-api-version", "1.7")
	req.Header.Set("hb-kyc-token", kycToken)
	req.Header.Set("hb-uc-token", ucToken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-client-platform", "android")
	req.Header.Set("huobi-app-version", "11.29.0")
	req.Header.Set("huobi-app-version-code", "112900")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "112900")
	req.Header.Set("user-agent", "BigHuobi/11.29.0 (Android 12; SM-F926U) Build/192 hbApp")
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("hb-uc-ua", ua)
	req.Header.Set("huobi-risefall-kline-utc", "8")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-website", "huobi.pro")
	req.Header.Set("homepagegray", "1")
	req.Header.Set("huobi-app-channel", "7890747")
	req.Header.Set("hb-country-id", "37")
	req.Header.Set("hb-region-id", "41")
	req.Header.Set("hb-ctx-id", uid)
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 处理gzip压缩响应
	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("解压gzip失败: %v", err)
		}
		defer reader.Close()
	default:
		reader = resp.Body
	}

	body, _ := io.ReadAll(reader)
	log.Printf("[PlaceGridOrder] 响应状态码: %d, 响应内容: %s", resp.StatusCode, string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析JSON失败: %v, 响应: %s", err, string(body))
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(string); ok && data != "" {
			return data, nil
		}
		// data 可能是数字
		if data, ok := result["data"].(float64); ok {
			return fmt.Sprintf("%d", int64(data)), nil
		}
	}

	return "", fmt.Errorf("下单失败，响应: %s", string(body))
}

// QueryGridStrategy 查询网格策略是否创建成功
func QueryGridStrategy(proToken, vtoken string, proxy string) (string, error) {
	r := fmt.Sprintf("%06d", rand.Intn(1000000))
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/hbg/v1/quantization/gridding/community-strategy-list?queryStrategyType=-1&type=1&limit=100&r=%s", r)

	client := newHTTPClient(proxy)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-api-version", "1.7")
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-client-platform", "android")
	req.Header.Set("huobi-app-version", "11.29.0")
	req.Header.Set("huobi-app-version-code", "112900")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "112900")
	req.Header.Set("user-agent", "BigHuobi/11.29.0 (Android 12; SM-F926U) Build/192 hbApp")
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("huobi-risefall-kline-utc", "8")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-website", "huobi.pro")
	req.Header.Set("homepagegray", "1")
	req.Header.Set("huobi-app-channel", "7890747")
	req.Header.Set("hb-country-id", "37")
	req.Header.Set("hb-region-id", "41")
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 处理gzip压缩响应
	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("解压gzip失败: %v", err)
		}
		defer reader.Close()
	default:
		reader = resp.Body
	}

	body, _ := io.ReadAll(reader)
	log.Printf("[QueryGridStrategy] 响应状态码: %d, 响应内容: %s", resp.StatusCode, string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析JSON失败: %v, 响应: %s", err, string(body))
	}

	// 检查错误码
	if code, ok := result["code"].(float64); ok && code != 200 {
		return "", fmt.Errorf("接口返回错误码: %v, 消息: %v, 响应: %s", code, result["message"], string(body))
	}

	if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
		if strategy, ok := data[0].(map[string]interface{}); ok {
			if profit, ok := strategy["totalProfit"]; ok {
				profitStr := fmt.Sprintf("%v", profit)
				if profitStr != "" && profitStr != "<nil>" {
					return profitStr, nil
				}
			}
		}
	}

	// 如果有数据但totalProfit为空，说明策略创建成功但还没有盈利
	if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
		return "创建策略成功", nil
	}

	return "", fmt.Errorf("未找到策略，响应: %s", string(body))
}

// MartingaleOrderRequest 马丁格尔下单请求体
type MartingaleOrderRequest struct {
	Symbol            string   `json:"symbol"`
	CopyID            int      `json:"copyId"`
	Source            string   `json:"source"`
	BuyPriceRate      float64  `json:"buyPriceRate"`
	SellPriceRate     float64  `json:"sellPriceRate"`
	BuyQuoteRate      string   `json:"buyQuoteRate"`
	LevelNum          string   `json:"levelNum"`
	InvestAmountQuote string   `json:"investAmountQuote"`
	RealQuoteAmount   int      `json:"realQuoteAmount"`
	StopLossRate      string   `json:"stopLossRate"`
	StrategyName      string   `json:"strategyName"`
	StrategyDesc      string   `json:"strategyDesc"`
	Coupons           []Coupon `json:"coupons"`
	CouponAmount      string   `json:"couponAmount"`
}

// PlaceMartingaleOrder 下单马丁格尔策略
func PlaceMartingaleOrder(proToken, kycToken, ucToken, ua, fingerprint, vtoken, uid, couponID string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/hbg/v1/quantization/gridding/martingale-commit"

	client := newHTTPClient(proxy)

	orderReq := MartingaleOrderRequest{
		Symbol:            "xrpusdt",
		CopyID:            0,
		Source:            "app_bots_spottrade",
		BuyPriceRate:      0.03,
		SellPriceRate:     0.02,
		BuyQuoteRate:      "1.5",
		LevelNum:          "2",
		InvestAmountQuote: "5",
		RealQuoteAmount:   0,
		StopLossRate:      "",
		StrategyName:      "",
		StrategyDesc:      "",
		Coupons:           []Coupon{{ID: couponID, Amount: "5"}},
		CouponAmount:      "5",
	}

	jsonBody, _ := json.Marshal(orderReq)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-api-version", "1.7")
	req.Header.Set("hb-kyc-token", kycToken)
	req.Header.Set("hb-uc-token", ucToken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-client-platform", "android")
	req.Header.Set("huobi-app-version", "11.29.0")
	req.Header.Set("huobi-app-version-code", "112900")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "112900")
	req.Header.Set("user-agent", "BigHuobi/11.29.0 (Android 12; SM-F926U) Build/192 hbApp")
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("hb-uc-ua", ua)
	req.Header.Set("huobi-risefall-kline-utc", "8")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-website", "huobi.pro")
	req.Header.Set("homepagegray", "1")
	req.Header.Set("huobi-app-channel", "7890747")
	req.Header.Set("hb-country-id", "37")
	req.Header.Set("hb-region-id", "41")
	req.Header.Set("hb-ctx-id", uid)
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 处理gzip压缩响应
	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("解压gzip失败: %v", err)
		}
		defer reader.Close()
	default:
		reader = resp.Body
	}

	body, _ := io.ReadAll(reader)
	log.Printf("[PlaceMartingaleOrder] 响应状态码: %d, 响应内容: %s", resp.StatusCode, string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析JSON失败: %v, 响应: %s", err, string(body))
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(string); ok && data != "" {
			return data, nil
		}
		if data, ok := result["data"].(float64); ok {
			return fmt.Sprintf("%d", int64(data)), nil
		}
	}

	return "", fmt.Errorf("马丁格尔下单失败，响应: %s", string(body))
}

// QueryMartingaleStrategy 查询马丁格尔策略
func QueryMartingaleStrategy(proToken, vtoken string, proxy string) (string, error) {
	r := fmt.Sprintf("%06d", rand.Intn(1000000))
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/hbg/v1/quantization/gridding/community-strategy-list?queryStrategyType=2&type=1&limit=100&r=%s", r)

	client := newHTTPClient(proxy)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-api-version", "1.7")
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-client-platform", "android")
	req.Header.Set("huobi-app-version", "11.29.0")
	req.Header.Set("huobi-app-version-code", "112900")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "112900")
	req.Header.Set("user-agent", "BigHuobi/11.29.0 (Android 12; SM-F926U) Build/192 hbApp")
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("huobi-risefall-kline-utc", "8")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-website", "huobi.pro")
	req.Header.Set("homepagegray", "1")
	req.Header.Set("huobi-app-channel", "7890747")
	req.Header.Set("hb-country-id", "37")
	req.Header.Set("hb-region-id", "41")
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 处理gzip压缩响应
	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("解压gzip失败: %v", err)
		}
		defer reader.Close()
	default:
		reader = resp.Body
	}

	body, _ := io.ReadAll(reader)
	log.Printf("[QueryMartingaleStrategy] 响应状态码: %d, 响应内容: %s", resp.StatusCode, string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析JSON失败: %v, 响应: %s", err, string(body))
	}

	// 检查错误码
	if code, ok := result["code"].(float64); ok && code != 200 {
		return "", fmt.Errorf("接口返回错误码: %v, 消息: %v, 响应: %s", code, result["message"], string(body))
	}

	if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
		if strategy, ok := data[0].(map[string]interface{}); ok {
			if profit, ok := strategy["totalProfit"]; ok {
				profitStr := fmt.Sprintf("%v", profit)
				if profitStr != "" && profitStr != "<nil>" {
					return profitStr, nil
				}
			}
		}
	}

	if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
		return "创建策略成功", nil
	}

	return "", fmt.Errorf("未找到马丁格尔策略，响应: %s", string(body))
}

// ProcessGridOrder 现货网格/马丁格尔下单完整流程
// strategyType: 0=现货网格, 1=马丁格尔
func ProcessGridOrder(email string, tk SavedToken, getProxy ProxyProvider, useProxy bool, strategyType int) (string, string, string, string, string, error) {
	time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)

	var proxy string
	if useProxy {
		proxy = getProxyWithRetry(getProxy, 3)
		if proxy == "" {
			return "", "", "", "", "", fmt.Errorf("获取代理失败")
		}
	}

	// 1. 获取 ticket
	ticket, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, proxy)
	if err != nil {
		log.Printf("[%s] 获取 ticket 失败: %v", email, err)
		return "", "", "", "", proxy, fmt.Errorf("获取 ticket 失败: %v", err)
	}
	log.Printf("[%s] 获取 ticket 成功: %s", email, ticket)

	// 2. 获取 proToken (token1)
	proToken, err := GetProToken(ticket, tk.UserAgent, proxy)
	if err != nil {
		log.Printf("[%s] 获取 proToken 失败: %v", email, err)
		return "", "", "", "", proxy, fmt.Errorf("获取 proToken 失败: %v", err)
	}
	log.Printf("[%s] 获取 proToken 成功: %s", email, proToken)

	// 3. 再次获取 ticket（获取kycToken需要新的ticket）
	ticket2, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, proxy)
	if err != nil {
		log.Printf("[%s] 获取第二次 ticket 失败: %v", email, err)
		return "", "", "", "", proxy, fmt.Errorf("获取第二次 ticket 失败: %v", err)
	}
	log.Printf("[%s] 获取 ticket2 成功: %s", email, ticket2)

	// 4. 获取 kycToken (token2)
	kycToken, err := GetKycToken(ticket2, tk.Fingerprint, tk.VToken, tk.UserAgent, proxy)
	if err != nil {
		log.Printf("[%s] 获取 kycToken 失败: %v", email, err)
		return "", "", "", "", proxy, fmt.Errorf("获取 kycToken 失败: %v", err)
	}
	log.Printf("[%s] 获取 kycToken 成功: %s", email, kycToken)

	// 5. 获取优惠券ID
	couponID, err := GetCouponID(proToken, kycToken, tk.UCToken, tk.UA, tk.Fingerprint, tk.VToken, tk.UID, proxy)
	if err != nil {
		log.Printf("[%s] 获取优惠券ID失败: %v", email, err)
		return "", "", "", "", proxy, fmt.Errorf("获取优惠券ID失败: %v", err)
	}
	log.Printf("[%s] 获取优惠券ID成功: %s", email, couponID)

	var strategyID string
	var orderErr error

	// 6. 根据策略类型下单
	if strategyType == 1 {
		// 马丁格尔
		log.Printf("[%s] 开始下单马丁格尔策略...", email)
		strategyID, orderErr = PlaceMartingaleOrder(proToken, kycToken, tk.UCToken, tk.UA, tk.Fingerprint, tk.VToken, tk.UID, couponID, proxy)
	} else {
		// 现货网格
		log.Printf("[%s] 开始下单现货网格策略...", email)
		strategyID, orderErr = PlaceGridOrder(proToken, kycToken, tk.UCToken, tk.UA, tk.Fingerprint, tk.VToken, tk.UID, couponID, proxy)
	}

	if orderErr != nil {
		log.Printf("[%s] 下单失败: %v", email, orderErr)
		return couponID, "", "", "", proxy, fmt.Errorf("下单失败: %v", orderErr)
	}
	log.Printf("[%s] 下单成功，策略单号: %s", email, strategyID)

	// 7. 延时0.5-1秒后查询
	delay := time.Duration(500+rand.Intn(500)) * time.Millisecond
	time.Sleep(delay)

	var profit string
	var queryErr error

	if strategyType == 1 {
		profit, queryErr = QueryMartingaleStrategy(proToken, tk.VToken, proxy)
	} else {
		profit, queryErr = QueryGridStrategy(proToken, tk.VToken, proxy)
	}

	if queryErr != nil {
		log.Printf("[%s] 查询收益失败: %v", email, queryErr)
		return couponID, strategyID, "", "下单成功但查询失败: " + queryErr.Error(), proxy, nil
	}
	log.Printf("[%s] 查询收益成功: %s", email, profit)

	strategyName := "现货网格"
	if strategyType == 1 {
		strategyName = "马丁格尔"
	}
	detail := fmt.Sprintf("[%s] 优惠券ID: %s, 策略单号: %s, 盈利: %s", strategyName, couponID, strategyID, profit)
	log.Printf("[%s] %s", email, detail)
	return couponID, strategyID, profit, detail, proxy, nil
}

// ProcessGridProfitQuery 单独查询收益流程
// strategyType: 0=现货网格, 1=马丁格尔
func ProcessGridProfitQuery(email string, tk SavedToken, getProxy ProxyProvider, useProxy bool, strategyType int) (string, string, error) {
	time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)

	var proxy string
	if useProxy {
		proxy = getProxyWithRetry(getProxy, 3)
		if proxy == "" {
			return "", "", fmt.Errorf("获取代理失败")
		}
	}

	// 1. 获取 ticket
	ticket, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, proxy)
	if err != nil {
		log.Printf("[%s] 查询收益-获取 ticket 失败: %v", email, err)
		return "", proxy, fmt.Errorf("获取 ticket 失败: %v", err)
	}
	log.Printf("[%s] 查询收益-获取 ticket 成功: %s", email, ticket)

	// 2. 获取 proToken
	proToken, err := GetProToken(ticket, tk.UserAgent, proxy)
	if err != nil {
		log.Printf("[%s] 查询收益-获取 proToken 失败: %v", email, err)
		return "", proxy, fmt.Errorf("获取 proToken 失败: %v", err)
	}
	log.Printf("[%s] 查询收益-获取 proToken 成功: %s", email, proToken)

	// 3. 查询策略收益
	var profit string
	if strategyType == 1 {
		log.Printf("[%s] 查询马丁格尔策略收益...", email)
		profit, err = QueryMartingaleStrategy(proToken, tk.VToken, proxy)
	} else {
		log.Printf("[%s] 查询现货网格策略收益...", email)
		profit, err = QueryGridStrategy(proToken, tk.VToken, proxy)
	}
	if err != nil {
		log.Printf("[%s] 查询收益失败: %v", email, err)
		return "", proxy, fmt.Errorf("查询收益失败: %v", err)
	}
	log.Printf("[%s] 查询收益成功: %s", email, profit)

	return profit, proxy, nil
}

// saveGridOrderResult 保存现货网格下单成功的账号
func saveGridOrderResult(email string, tk SavedToken) {
	fileName := "grid_order_success.txt"
	jsonFilePath := "grid_order_success.json"

	// 去重检查
	if _, err := os.Stat(fileName); err == nil {
		data, err := os.ReadFile(fileName)
		if err == nil {
			if strings.Contains(string(data), email+"----") {
				log.Printf("[现货网格] 账号 %s 已存在，跳过保存", email)
				return
			}
		}
	}

	// 保存txt文件
	file, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[现货网格] 打开文件失败: %v", err)
		return
	}
	defer file.Close()

	content := fmt.Sprintf("%s----%s----%s----%s\n", email, tk.Password, tk.UID, tk.GAKey)
	if _, err := file.WriteString(content); err != nil {
		log.Printf("[现货网格] 写入文件失败: %v", err)
	} else {
		log.Printf("[现货网格] 已保存账号 %s", email)
	}

	// 保存JSON文件
	tokenLock.Lock()
	defer tokenLock.Unlock()

	var tokens map[string]SavedToken
	data, err := os.ReadFile(jsonFilePath)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &tokens); err != nil {
			tokens = make(map[string]SavedToken)
		}
	} else {
		tokens = make(map[string]SavedToken)
	}

	if _, exists := tokens[email]; !exists {
		tokens[email] = SavedToken{
			Email:       email,
			Password:    tk.Password,
			GAKey:       tk.GAKey,
			UCToken:     tk.UCToken,
			VToken:      tk.VToken,
			Fingerprint: tk.Fingerprint,
			UA:          tk.UA,
			UserAgent:   tk.UserAgent,
			UID:         tk.UID,
			LastLogin:   time.Now(),
		}

		data, err = json.MarshalIndent(tokens, "", "  ")
		if err != nil {
			log.Printf("[现货网格] 序列化JSON失败: %v", err)
			return
		}

		if err := os.WriteFile(jsonFilePath, data, 0644); err != nil {
			log.Printf("[现货网格] 写入JSON文件失败: %v", err)
		}
	}
}
