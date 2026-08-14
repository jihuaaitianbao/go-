package main

import (
	"bytes"
	"crypto/md5"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const RSAPublicKey = "MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCulDIsWM5Fgv0VNAQZbjhRdnSvc0+ICzezd5Q/2hL+oKCR2z8+Lm3O/ZCRIXTyFnDt3m2yvSueZyt8hCuIV+JKBM+5KJkIH2MlOEOsMTRaGPzhWdkLUb2j4DbcSmPcyXMP9TwVTgoGd0ISbxf1hZngsk0poy/1rCw+u4iLdxvt1QIDAQAB"

type DeviceProfile struct {
	DeviceFingerprint string
}

func NewDeviceProfile() DeviceProfile {
	// 按照指定格式生成指纹：ffffffff-xxxx-xxxx-ffff-ffffxxxxxxxx
	// 注意：randomHex(n) 生成 n 字节 = 2n 个十六进制字符
	part1 := "ffffffff"
	part2 := randomHex(2) // 2 字节 = 4 个十六进制字符
	part3 := randomHex(2) // 2 字节 = 4 个十六进制字符
	part4 := "ffff"
	part5 := "ffff" + randomHex(4) // 4 字节 = 8 个十六进制字符

	fingerprint := part1 + "-" + part2 + "-" + part3 + "-" + part4 + "-" + part5

	return DeviceProfile{
		DeviceFingerprint: fingerprint,
	}
}

type HTXLoginManager struct {
	Profile     DeviceProfile
	Proxy       string
	HTTPClient  *http.Client
	VToken      string
	Fingerprint string
	X3          string
	UA          string
	VHash       string
	UserAgent   string

	// OnStatus 用于实时推送登录步骤状态（供GUI更新表格）
	OnStatus func(string)
}

// callStatus 安全调用状态回调
func (m *HTXLoginManager) callStatus(msg string) {
	if m.OnStatus != nil {
		m.OnStatus(msg)
	}
	log.Printf("[STATUS] %s", msg)
}

// RefreshProxy 更换代理并重建 HTTPClient（用于登录失败重试时切换代理）
func (m *HTXLoginManager) RefreshProxy(newProxy string) {
	m.Proxy = newProxy

	transport, err := createProxyTransport(newProxy)
	if err != nil {
		log.Printf("[RefreshProxy] 代理初始化失败: %v，将使用默认 transport", err)
		m.HTTPClient = &http.Client{Timeout: 30 * time.Second}
		return
	}

	m.HTTPClient = &http.Client{Timeout: 30 * time.Second, Transport: transport}
	log.Printf("[RefreshProxy] 已配置代理: %s", newProxy)
}

func randomHex(n int) string {
	b := make([]byte, n)
	crand.Read(b)
	return hex.EncodeToString(b)
}

func md5Hash(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

var androidDevices = []string{
	"M2011K2C", "SM-A125F", "SM-S9080", "MI 12", "MI 11", "SM-G9900",
	"SM-A525F", "RMX3366", "OPPO Reno7", "vivo X80", "Redmi Note 11",
	"SM-A736B", "M2102K1AC", "SM-G9880", "OPPO Find X5", "vivo X70",
	"Redmi K50", "SM-A536B", "M2203L1AC", "SM-G9730", "OPPO Reno8",
}

var androidVersions = []string{"12", "13", "14"}

var buildNumbers = []string{"144", "159", "168", "175", "189", "198", "205", "212"}

func generateRandomUserAgent() string {
	device := androidDevices[rand.Intn(len(androidDevices))]
	version := androidVersions[rand.Intn(len(androidVersions))]
	build := buildNumbers[rand.Intn(len(buildNumbers))]
	return fmt.Sprintf("BigHuobi/11.20.0 (Android %s; %s) Build/%s hbApp", version, device, build)
}

func NewHTXLoginManager(profile DeviceProfile, proxy string) *HTXLoginManager {
	// 如果 fingerprint 为空，生成一个新的
	if profile.DeviceFingerprint == "" {
		profile.DeviceFingerprint = randomHex(32)
	}

	vtoken := randomHex(16)

	// 创建 HTTP 客户端，支持代理
	transport, err := createProxyTransport(proxy)
	if err != nil {
		log.Printf("[NewHTXLoginManager] 代理初始化失败: %v，将使用默认 transport", err)
	}

	var httpClient *http.Client
	if err != nil || transport == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	} else {
		httpClient = &http.Client{Timeout: 30 * time.Second, Transport: transport}
		log.Printf("[NewHTXLoginManager] 已配置代理: %s", proxy)
	}

	return &HTXLoginManager{
		Profile:     profile,
		Proxy:       proxy,
		HTTPClient:  httpClient,
		VToken:      vtoken,
		Fingerprint: profile.DeviceFingerprint,
		X3:          randomHex(16),
		UA:          randomHex(16),
		VHash:       md5Hash(vtoken),
		UserAgent:   generateRandomUserAgent(),
	}
}

func (m *HTXLoginManager) getHeaders() map[string]string {
	return map[string]string{
		"accept-language":          "zh-CN",
		"apptype":                  "1",
		"huobi-app-client":         "2",
		"huobi-app-version":        "11.20.0",
		"huobi-app-version-code":   "112000",
		"appversion":               "112000",
		"user-agent":               m.UserAgent,
		"huobi-timezone":           "GMT+08:00",
		"terminalid":               "1",
		"vop":                      "0",
		"device-v-token":           m.Fingerprint,
		"huobi-website":            "huobi.pro",
		"huobi-app-channel":        "7890747",
		"hb-country-id":            "37",
		"hb-region-id":             "41",
		"x-b3-traceid":             m.X3,
		"huobi-client-platform":    "ANDROID",
		"huobi-client-fingerprint": m.Fingerprint,
		"hb-uc-ua":                 m.UA,
		"vtoken":                   m.VToken,
		"content-type":             "application/json; charset=UTF-8",
		"accept-encoding":          "gzip",
	}
}

func (m *HTXLoginManager) crackGeetestV4(captchaID string, proxyIP string) map[string]interface{} {
	url := "http://127.0.0.1:8989"
	payload := fmt.Sprintf("火币,%s,%s", proxyIP, captchaID)
	log.Printf("[DEBUG] 极验破解请求: %s", payload)

	req, err := http.NewRequest("POST", url, bytes.NewBufferString(payload))
	if err != nil {
		log.Printf("创建请求失败: %v", err)
		return nil
	}
	req.Header.Set("Content-Type", "text/plain")

	localClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := localClient.Do(req)
	if err != nil {
		log.Printf("极验破解失败: %v", err)
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil
	}
	seccode, ok := data["seccode"].(map[string]interface{})
	if !ok {
		return nil
	}

	return seccode
}

func (m *HTXLoginManager) LoginFlow(loginName, passwordRaw, googleKey string) map[string]interface{} {
	log.Printf("开始登录流程: %s", loginName)
	m.callStatus("风险控制检查中...")

	// 1. 风险控制检查获取 captcha_id
	vhashURL := fmt.Sprintf("https://www.htx.net.im/-/x/uc/uc/open/risk/control?vHash=%s", m.VHash)

	p0Params := map[string]string{
		"app_v":   "11.20.0",
		"brand":   "Xiaomi",
		"p_type":  "MI 6",
		"sdk_v":   "1.3.1",
		"sys":     "Android",
		"sys_ver": "10",
		"wm":      "7890747",
	}
	p0, k0, err := GenerateP0K0Dynamic(m.VToken, p0Params, RSAPublicKey)
	if err != nil {
		log.Printf("生成 P0/K0 失败: %v", err)
		return nil
	}

	riskData := map[string]interface{}{
		"p0":           p0,
		"country_code": "0086",
		"login_name":   loginName,
		"cHash":        m.VHash,
		"k0":           k0,
		"fingerprint":  m.Fingerprint,
		"source":       5,
		"vToken":       m.VToken,
		"version":      "4",
		"scene":        2,
	}

	riskBody, _ := json.Marshal(riskData)
	req, _ := http.NewRequest("POST", vhashURL, bytes.NewBuffer(riskBody))
	for k, v := range m.getHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := m.HTTPClient.Do(req)
	if err != nil {
		log.Printf("风险控制请求失败: %v", err)
		return nil
	}
	defer resp.Body.Close()

	var riskResult map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&riskResult)

	var captchaID string
	if data, ok := riskResult["data"].(map[string]interface{}); ok {
		if items, ok := data["itemsv3"].([]interface{}); ok && len(items) > 1 {
			if item, ok := items[1].(map[string]interface{}); ok {
				if props, ok := item["properties"].(map[string]interface{}); ok {
					if cid, ok := props["captcha_id"].(string); ok {
						captchaID = cid
					}
				}
			}
		}
	}

	if captchaID == "" {
		log.Println("未能获取 captcha_id")
		return nil
	}
	log.Printf("获取到 captcha_id: %s", captchaID)
	m.callStatus("极验破解中...")

	// 2. 极验破解
	// 提取代理 IP 地址（如果是 http://ip:port 格式）
	proxyIP := m.Proxy
	if proxyIP != "" {
		// 仅提取 IP 部分
		if strings.Contains(proxyIP, "://") {
			parts := strings.Split(proxyIP, "://")
			if len(parts) > 1 {
				proxyIP = parts[1]
			}
		}
	}
	geetestData := m.crackGeetestV4(captchaID, proxyIP)
	if geetestData == nil || geetestData["lot_number"] == nil {
		log.Println("极验破解未返回有效数据")
		return nil
	}

	// 3. 获取 Flow Token
	flowURL := "https://www.htx.net.im/-/x/uc/uc/open/login/flow_type"
	flowData := map[string]interface{}{
		"country_code": "0086",
		"login_name":   loginName,
		"captcha_param": map[string]interface{}{
			"params": map[string]interface{}{
				"captcha_id":     captchaID,
				"gen_time":       geetestData["gen_time"],
				"captcha_output": geetestData["captcha_output"],
				"pass_token":     geetestData["pass_token"],
				"lot_number":     geetestData["lot_number"],
			},
			"type": "3",
		},
	}

	flowBody, _ := json.Marshal(flowData)
	//log.Printf("[DEBUG] Flow Token 请求URL: %s", flowURL)
	//log.Printf("[DEBUG] Flow Token 请求体: %s", string(flowBody))
	req, _ = http.NewRequest("POST", flowURL, bytes.NewBuffer(flowBody))
	for k, v := range m.getHeaders() {
		req.Header.Set(k, v)
	}
	//log.Printf("[DEBUG] Flow Token 请求头已设置")

	resp, err = m.HTTPClient.Do(req)
	if err != nil {
		log.Printf("获取 Flow Token 失败: %v", err)
		return nil
	}
	defer resp.Body.Close()

	var flowResult map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&flowResult)

	var loginToken string
	if data, ok := flowResult["data"].(map[string]interface{}); ok {
		if token, ok := data["auth_code_login_token"].(string); ok {
			loginToken = token
		}
	}

	if loginToken == "" {
		log.Println("未能获取 auth_code_login_token")
		return nil
	}
	log.Printf("获取到 login_token: %s", loginToken)
	m.callStatus("密码登录中...")

	// 4. 密码登录
	passwordMD5 := md5Hash(passwordRaw + "hello, moto")
	loginURL := fmt.Sprintf("https://www.htx.net.im/-/x/uc/uc/open/login?vHash=%s", m.VHash)
	log.Printf("[DEBUG] 密码登录URL: %s", loginURL)
	log.Printf("[DEBUG] 密码MD5: %s", passwordMD5)

	loginPayload := map[string]interface{}{
		"p0":    p0,
		"cHash": m.VHash,
		"k0":    k0,
		"login_ext_data": map[string]interface{}{
			"af_id":             "1767941287833-5805022930860536886",
			"app_instance_id":   "880c34013532ba6a2451a31659deadd1",
			"af_app_id":         "pro.huobi",
			"af_device_id_type": "oaid",
			"af_device_id":      "",
		},
		"vToken":      m.VToken,
		"way":         "APP_HUOBI_PRO",
		"token":       loginToken,
		"vtoken":      m.VToken,
		"password":    passwordMD5,
		"login_name":  loginName,
		"fingerprint": m.Fingerprint,
		"captcha_param": map[string]interface{}{
			"params": map[string]interface{}{
				"captcha_id":     captchaID,
				"gen_time":       geetestData["gen_time"],
				"captcha_output": geetestData["captcha_output"],
				"pass_token":     geetestData["pass_token"],
				"lot_number":     geetestData["lot_number"],
			},
			"type": "3",
		},
		"ga_switch":     true,
		"login_version": 3,
	}

	loginBody, _ := json.Marshal(loginPayload)
	//log.Printf("[DEBUG] 登录请求体: %s", string(loginBody))
	req, _ = http.NewRequest("POST", loginURL, bytes.NewBuffer(loginBody))
	headers := m.getHeaders()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// 打印所有请求头

	resp, err = m.HTTPClient.Do(req)
	if err != nil {
		log.Printf("密码登录请求失败: %v", err)
		return nil
	}
	defer resp.Body.Close()

	log.Printf("[DEBUG] 密码登录响应状态码: %d", resp.StatusCode)

	var loginResult map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	//log.Printf("[DEBUG] 密码登录原始响应体: %s", string(body))
	json.Unmarshal(body, &loginResult)

	// 调试：打印完整响应
	//log.Printf("[DEBUG] 密码登录响应: %v", loginResult)

	var token2FA string
	if data, ok := loginResult["data"].(map[string]interface{}); ok {
		if token, ok := data["token"].(string); ok {
			token2FA = token
		}
	}

	if token2FA == "" {
		log.Printf("登录失败，未能获取 2FA Token。完整响应: %v", loginResult)
		return nil
	}
	log.Printf("登录成功，获取到 2FA Token: %s", token2FA)
	m.callStatus("密码登录成功，GA验证中...")

	// 5. GA 二步验证
	if googleKey == "" {
		log.Println("未提供 GA Key，无法完成二步验证")
		return map[string]interface{}{"success": true, "need_ga": true, "token": token2FA}
	}

	gaCode := GetGACode(googleKey)
	log.Printf("生成 GA 验证码: %s", gaCode)

	gaURL := fmt.Sprintf("https://www.htx.net.im/-/x/uc/uc/open/2fa/login?vHash=%s", m.VHash)
	gaPayload := map[string]interface{}{
		"p0":           p0,
		"vtoken":       m.VToken,
		"ga_code":      gaCode,
		"cHash":        m.VHash,
		"k0":           k0,
		"isKnowDevice": false,
		"login_ext_data": map[string]interface{}{
			"af_id":             "1767941287833-5805022930860536886",
			"app_instance_id":   "880c34013532ba6a2451a31659deadd1",
			"af_app_id":         "pro.huobi",
			"af_device_id_type": "oaid",
			"af_device_id":      "",
		},
		"vToken":        m.VToken,
		"token":         token2FA,
		"login_version": 4,
	}

	gaBody, _ := json.Marshal(gaPayload)
	req, _ = http.NewRequest("POST", gaURL, bytes.NewBuffer(gaBody))
	for k, v := range m.getHeaders() {
		req.Header.Set(k, v)
	}

	resp, err = m.HTTPClient.Do(req)
	if err != nil {
		log.Printf("GA 验证请求失败: %v", err)
		return nil
	}
	defer resp.Body.Close()

	var gaResult map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&gaResult)

	var ucToken string
	if data, ok := gaResult["data"].(map[string]interface{}); ok {
		if token, ok := data["uc_token"].(string); ok {
			ucToken = token
		}
	}

	if ucToken != "" {
		log.Printf("GA 验证成功！获取到 uc_token: %s", ucToken)
		m.callStatus("GA验证成功，登录完成")
		return map[string]interface{}{"success": true, "uc_token": ucToken, "vtoken": m.VToken, "user_agent": m.UserAgent}
	}

	log.Printf("GA 验证失败: %v", gaResult["message"])
	return map[string]interface{}{"success": false, "message": gaResult["message"]}
}

// main 函数已移动到 run_test.go
