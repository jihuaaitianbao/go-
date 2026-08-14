//go:build web

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beevik/ntp"
	"github.com/gin-gonic/gin"
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        500,
		MaxIdleConnsPerHost: 200,
		IdleConnTimeout:     90 * time.Second,
	},
}

type Account struct {
	Email    string
	Password string
	UID      string
	GAKey    string
}

type LoginResult struct {
	ID       int    `json:"id"`
	Email    string `json:"email"`
	VToken   string `json:"vtoken"`
	ProToken string `json:"protoken"`
	UCToken  string `json:"uctoken"`
	Status   string `json:"status"`
	Result   string `json:"result"`
}

type taskStatusType struct {
	Running   bool   `json:"running"`
	Message   string `json:"message"`
	Processed int    `json:"processed"`
	Total     int    `json:"total"`
}

var taskStatus = taskStatusType{Running: false, Message: "就绪", Processed: 0, Total: 0}
var statusMu sync.Mutex
var stopChan chan struct{}

type ProxyPool struct {
	pool      chan string
	proxyURL  string
	lastFetch time.Time
	mu        sync.Mutex
}

var globalProxyPool *ProxyPool

func initProxyPool(proxyURL string) {
	globalProxyPool = &ProxyPool{
		pool:     make(chan string, 1000),
		proxyURL: proxyURL,
	}
}

func (p *ProxyPool) fetchFromPool() string {
	select {
	case proxy := <-p.pool:
		return proxy
	default:
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case proxy := <-p.pool:
		return proxy
	default:
	}

	now := time.Now()
	if now.Sub(p.lastFetch) < time.Second {
		time.Sleep(time.Second - now.Sub(p.lastFetch))
	}

	proxies, err := fetchProxyList(p.proxyURL)
	if err != nil || len(proxies) == 0 {
		return ""
	}

	p.lastFetch = time.Now()

	for _, proxy := range proxies {
		select {
		case p.pool <- proxy:
		default:
		}
	}

	return proxies[0]
}

func getFreshProxyWithURL(proxyFetchURL string) string {
	if proxyFetchURL == "" {
		return ""
	}

	if globalProxyPool == nil || globalProxyPool.proxyURL != proxyFetchURL {
		initProxyPool(proxyFetchURL)
	}

	return globalProxyPool.fetchFromPool()
}

type ResultItem struct {
	Email      string  `json:"email"`
	Result     string  `json:"result"`
	USDT       string  `json:"usdt"`
	Badge      string  `json:"badge"`
	Timestamp  string  `json:"timestamp"`
	SortValue  float64 `json:"sort_value"`
	CouponID   string  `json:"coupon_id,omitempty"`
	StrategyID string  `json:"strategy_id,omitempty"`
	Profit     string  `json:"profit,omitempty"`
	IP         string  `json:"ip,omitempty"`
	Detail     string  `json:"detail,omitempty"`
}

type SignInResult struct {
	Email             string `json:"email"`
	Status            string `json:"status"`
	WelfareGold       string `json:"welfare_gold"`
	BadgeBefore       string `json:"badge_before"`
	WelfareSignStatus string `json:"welfare_sign_status"`
	InviteSignStatus  string `json:"invite_sign_status"`
	BadgeCount        string `json:"badge_count"`
	OperationTime     string `json:"operation_time"`
}

var loginResults []LoginResult
var resultsMap = map[string][]ResultItem{
	"exchange":  []ResultItem{},
	"hongbao":   []ResultItem{},
	"return":    []ResultItem{},
	"turntable": []ResultItem{},
	"coupon":    []ResultItem{},
	"grid":      []ResultItem{},
}
var signInResults []SignInResult
var signInResultsMu sync.Mutex

type LoginRequest struct {
	Concurrency   int    `json:"concurrency" form:"concurrency"`
	ProxyFetchURL string `json:"proxy_fetch_url" form:"proxy_fetch_url"`
	ProxyURL      string `json:"proxy_url" form:"proxy_url"`
	UseProxy      bool   `json:"use_proxy" form:"use_proxy"`
}

type ExchangeRequest struct {
	Concurrency   int  `json:"concurrency" form:"concurrency"`
	Hour          int  `json:"hour" form:"hour"`
	Minute        int  `json:"minute" form:"minute"`
	Second        int  `json:"second" form:"second"`
	RetryCount    int  `json:"retry_count" form:"retry_count"`
	UseProxy      bool `json:"use_proxy" form:"use_proxy"`
	AdvanceMs     int  `json:"advance_ms" form:"advance_ms"`
	UseServerTime bool `json:"use_server_time" form:"use_server_time"`
}

type TaskRequest struct {
	Concurrency int  `json:"concurrency" form:"concurrency"`
	UseProxy    bool `json:"use_proxy" form:"use_proxy"`
}

type TurntableRequest struct {
	Concurrency int    `json:"concurrency" form:"concurrency"`
	ActivityId  string `json:"activity_id" form:"activity_id"`
	UseSchedule bool   `json:"use_schedule" form:"use_schedule"`
	Hour        int    `json:"hour" form:"hour"`
	Minute      int    `json:"minute" form:"minute"`
	Second      int    `json:"second" form:"second"`
}

func getIndexHTML() string {
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>HTX 助手 - Web版</title>
<style>
*{margin:0;padding:0;box-sizing:border-box;}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);min-height:100vh;padding:20px;}
.container{max-width:1400px;margin:0 auto;}
.header{text-align:center;color:white;margin-bottom:30px;}
.header h1{font-size:32px;margin-bottom:10px;}
.header p{opacity:0.8;}
.tabs{display:flex;justify-content:center;gap:10px;margin-bottom:20px;flex-wrap:wrap;}
.tab-btn{padding:12px 25px;border:none;border-radius:25px;background:rgba(255,255,255,0.2);color:white;font-size:16px;cursor:pointer;transition:all 0.3s;}
.tab-btn:hover{background:rgba(255,255,255,0.3);}
.tab-btn.active{background:white;color:#667eea;}
.tab-content{display:none;background:white;border-radius:15px;padding:30px;box-shadow:0 10px 40px rgba(0,0,0,0.2);}
.tab-content.active{display:block;}
.control-panel{display:flex;flex-wrap:wrap;gap:15px;align-items:center;margin-bottom:25px;padding:20px;background:#f8f9fa;border-radius:10px;}
.control-panel label{font-weight:600;color:#333;}
.control-panel input[type="number"],.control-panel input[type="text"]{padding:8px 12px;border:1px solid #ddd;border-radius:6px;font-size:14px;}
.control-panel input[type="number"]{width:100px;}
.control-panel input[type="text"]{width:200px;}
.control-panel input[type="checkbox"]{width:20px;height:20px;}
.action-btn{padding:10px 25px;border:none;border-radius:8px;font-size:15px;font-weight:600;cursor:pointer;transition:all 0.3s;}
.btn-primary{background:linear-gradient(135deg,#667eea 0%,#764ba2 100%);color:white;}
.btn-primary:hover:not(:disabled){transform:translateY(-2px);box-shadow:0 5px 20px rgba(102,126,234,0.4);}
.btn-primary:disabled{opacity:0.5;cursor:not-allowed;}
.btn-danger{background:linear-gradient(135deg,#ef4444 0%,#dc2626 100%);color:white;}
.btn-danger:hover:not(:disabled){transform:translateY(-2px);box-shadow:0 5px 20px rgba(239,68,68,0.4);}
.btn-secondary{background:#6c757d;color:white;}
.btn-secondary:hover{background:#5a6268;}
.sort-btn{background:#17a2b8;color:white;padding:10px 20px;border:none;border-radius:8px;font-size:14px;font-weight:600;cursor:pointer;transition:all 0.3s;}
.sort-btn:hover{background:#138496;}
.sort-btn.active-sort{background:#fd7e14;}
.status-bar{padding:12px 20px;border-radius:8px;margin-bottom:20px;font-weight:500;}
.status-idle{background:#d4edda;color:#155724;}
.status-running{background:#fff3cd;color:#856404;}
.status-done{background:#d1ecf1;color:#0c5460;}
.info-card{background:#e7f3ff;padding:15px;border-radius:8px;margin-bottom:20px;border-left:4px solid #007bff;}
.info-card strong{color:#007bff;}
.results-table{width:100%;border-collapse:collapse;margin-top:15px;font-size:13px;}
.results-table th,.results-table td{padding:10px;text-align:left;border-bottom:1px solid #eee;}
.results-table th{background:#f8f9fa;font-weight:600;color:#495057;}
.results-table tr:hover{background:#f8f9fa;}
.results-table td{max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}
.badge-success{background:#d4edda;color:#155724;padding:2px 8px;border-radius:4px;font-size:12px;}
.badge-error{background:#f8d7da;color:#721c24;padding:2px 8px;border-radius:4px;font-size:12px;}
.badge-warning{background:#fff3cd;color:#856404;padding:2px 8px;border-radius:4px;font-size:12px;}
.stats{display:flex;gap:20px;margin-bottom:20px;}
.stat-item{background:#f8f9fa;padding:15px 25px;border-radius:10px;text-align:center;}
.stat-item .number{font-size:28px;font-weight:bold;color:#667eea;}
.stat-item .label{font-size:14px;color:#666;margin-top:5px;}
@media(max-width:768px){.control-panel{flex-direction:column;align-items:flex-start;}.tabs{flex-wrap:wrap;}.tab-btn{flex:1;min-width:100px;}.stats{flex-wrap:wrap;}}
.global-status-bar{position:fixed;bottom:0;left:0;right:0;background:#2d3748;color:white;padding:15px 20px;display:flex;align-items:center;gap:20px;box-shadow:0 -5px 20px rgba(0,0,0,0.2);z-index:1000;}
.global-status-bar #global-status-text{font-weight:600;min-width:150px;}
.global-status-bar #global-progress{flex:1;height:8px;background:rgba(255,255,255,0.2);border-radius:4px;overflow:hidden;}
.global-status-bar #global-progress-bar{height:100%;background:linear-gradient(90deg,#667eea,#764ba2);width:0%;transition:width 0.3s;}
.global-status-bar #global-stats{font-size:14px;color:rgba(255,255,255,0.8);}
.global-status-bar #global-stats span{font-weight:bold;color:white;}
</style>
</head>
<body>
<div class="container">
<div class="header"><h1>HTX 助手</h1><p>登录管理 · 兑换勋章 · 签到 · 红包雨 · 领取回归奖励</p></div>
<div class="tabs">
<button class="tab-btn active" data-tab="login" onclick="switchTab('login')">登录管理</button>
<button class="tab-btn" data-tab="exchange" onclick="switchTab('exchange')">兑换勋章</button>
<button class="tab-btn" data-tab="signin" onclick="switchTab('signin')">签到</button>
<button class="tab-btn" data-tab="hongbao" onclick="switchTab('hongbao')">红包雨</button>
<button class="tab-btn" data-tab="turntable" onclick="switchTab('turntable')">大转盘抽奖</button>
<button class="tab-btn" data-tab="coupon" onclick="switchTab('coupon')">查询优惠券</button>
<button class="tab-btn" data-tab="grid" onclick="switchTab('grid')">现货网格下单</button>
<button class="tab-btn" data-tab="asset" onclick="switchTab('asset')">查询资产</button>
<button class="tab-btn" data-tab="return" onclick="switchTab('return')">领取回归奖励</button>
<button class="tab-btn" data-tab="probe" onclick="switchTab('probe')">代理测速</button>
</div>
<div id="login" class="tab-content active">
<div class="info-card"><strong>登录管理说明：</strong>从accounts.txt读取账号，自动登录获取Token并保存到tokens.json，支持代理配置和并发控制。</div>
<div class="control-panel">
<label>代理提取URL：</label><input type="text" id="login-proxy-fetch-url" placeholder="http://example.com/get_proxy" style="width:250px;">
<label>并发线程数：</label><input type="number" id="login-concurrency" value="200" min="1" max="500">
<label><input type="checkbox" id="login-use-proxy"> 使用固定代理</label>
<input type="text" id="login-proxy-url" placeholder="http://ip:port" disabled>
<button class="action-btn btn-primary" id="login-btn" onclick="startLogin()">开始登录</button>
<button class="action-btn btn-danger" onclick="stopTask()">停止执行</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
</div>
<div id="login-status" class="status-bar status-idle">准备就绪</div>
<div class="stats">
<div class="stat-item"><div class="number" id="login-total">0</div><div class="label">账号总数</div></div>
<div class="stat-item"><div class="number" id="login-success">0</div><div class="label">成功</div></div>
<div class="stat-item"><div class="number" id="login-fail">0</div><div class="label">失败</div></div>
</div>
<div id="login-results"></div>
</div>
<div id="exchange" class="tab-content">
<div class="info-card"><strong>兑换勋章说明：</strong>预加载pro_token，NTP对时+HTX库存刷新时间精确定时，全量同时发送，支持提前ms补偿网络RTT。</div>
<div class="control-panel">
<label>目标时间：</label>
<input type="number" id="exchange-hour" value="0" min="0" max="23" style="width:60px;">时
<input type="number" id="exchange-minute" value="0" min="0" max="59" style="width:60px;">分
<input type="number" id="exchange-second" value="0" min="0" max="59" style="width:60px;">秒
<label><input type="checkbox" id="exchange-use-server-time" checked>用服务器刷新时间</label>
<label>提前ms：</label><input type="number" id="exchange-advance-ms" value="55" min="0" max="5000" style="width:80px;">
<label>每个账号发送次数：</label><input type="number" id="exchange-retry" value="10" min="1" max="100">
<label><input type="checkbox" id="exchange-use-proxy" checked>使用代理</label>
<button class="action-btn btn-primary" id="exchange-btn" onclick="startExchange()">开始兑换</button>
<button class="action-btn btn-danger" onclick="stopTask()">停止执行</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
</div>
<div id="exchange-status" class="status-bar status-idle">准备就绪</div>
<div id="exchange-countdown" style="font-size:18px;font-weight:bold;color:#667eea;margin-bottom:20px;text-align:center;"></div>
<div class="stats">
<div class="stat-item"><div class="number" id="exchange-total">0</div><div class="label">账号总数</div></div>
<div class="stat-item"><div class="number" id="exchange-success">0</div><div class="label">成功</div></div>
<div class="stat-item"><div class="number" id="exchange-fail">0</div><div class="label">失败</div></div>
</div>
<div id="exchange-results"></div>
</div>
<div id="signin" class="tab-content">
<div class="info-card"><strong>签到说明：</strong>自动为所有账号执行签到，查询福利金余额，高福利金账号会自动保存。支持按福利金降序排序并一键保存到 tokens.json。</div>
<div class="control-panel">
<label>并发线程数：</label><input type="number" id="signin-concurrency" value="10" min="1" max="100">
<label><input type="checkbox" id="signin-use-proxy" checked>使用代理</label>
<button class="action-btn btn-primary" id="signin-btn" onclick="startSignIn()">开始签到</button>
<button class="action-btn btn-danger" onclick="stopTask()">停止执行</button>
<button class="action-btn btn-secondary" onclick="retryFailedSignIn()">重新运行失败账号</button>
<button class="action-btn btn-secondary" onclick="retryLoginFailed()">重新登录失败账号</button>
<button class="action-btn btn-info" onclick="sortSignInByWelfare()">按福利金排序</button>
<button class="action-btn btn-success" onclick="saveSignInOrder()">保存排序后顺序</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
</div>
<div id="signin-status" class="status-bar status-idle">准备就绪</div>
<div class="stats">
<div class="stat-item"><div class="number" id="signin-total">0</div><div class="label">账号总数</div></div>
<div class="stat-item"><div class="number" id="signin-success">0</div><div class="label">成功</div></div>
<div class="stat-item"><div class="number" id="signin-fail">0</div><div class="label">失败</div></div>
</div>
<div id="signin-results"></div>
</div>
<div id="hongbao" class="tab-content">
<div class="info-card"><strong>红包雨说明：</strong>自动为所有账号执行红包雨领取，领取结果会保存到本地文件。</div>
<div class="control-panel">
<label>并发线程数：</label><input type="number" id="hongbao-concurrency" value="8" min="1" max="100">
<label><input type="checkbox" id="hongbao-use-schedule"> 启用定时</label>
<label>目标时间：</label>
<input type="number" id="hongbao-hour" value="0" min="0" max="23" style="width:60px;">时
<input type="number" id="hongbao-minute" value="0" min="0" max="59" style="width:60px;">分
<input type="number" id="hongbao-second" value="0" min="0" max="59" style="width:60px;">秒
<button class="action-btn btn-primary" id="hongbao-btn" onclick="startHongBao()">开始红包雨</button>
<button class="action-btn btn-danger" onclick="stopTask()">停止执行</button>
<button class="action-btn btn-secondary" onclick="extractAccounts()">提取账号</button>
<input type="text" id="token-file-path" placeholder="tokens文件路径" style="width:200px;">
<button class="action-btn btn-secondary" onclick="switchTokenFile()">切换tokens文件</button>
<button class="action-btn btn-secondary" onclick="useDefaultTokenFile()">使用默认tokens</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
</div>
<div id="hongbao-status" class="status-bar status-idle">准备就绪</div>
<div id="hongbao-countdown" style="font-size:18px;font-weight:bold;color:#667eea;margin-bottom:20px;text-align:center;"></div>
<div class="stats">
<div class="stat-item"><div class="number" id="hongbao-total">0</div><div class="label">账号总数</div></div>
<div class="stat-item"><div class="number" id="hongbao-success">0</div><div class="label">成功</div></div>
<div class="stat-item"><div class="number" id="hongbao-fail">0</div><div class="label">失败</div></div>
</div>
<div id="hongbao-results"></div>
</div>
<div id="turntable" class="tab-content">
<div class="info-card"><strong>大转盘抽奖说明：</strong>自动为所有账号执行大转盘抽奖，需填写活动ID，支持定时执行。</div>
<div class="control-panel">
<label>抽奖活动ID：</label><input type="text" id="turntable-activity-id" placeholder="请输入活动ID" style="width:220px;">
<label>并发线程数：</label><input type="number" id="turntable-concurrency" value="8" min="1" max="100">
<label><input type="checkbox" id="turntable-use-schedule"> 启用定时</label>
<label>目标时间：</label>
<input type="number" id="turntable-hour" value="0" min="0" max="23" style="width:60px;">时
<input type="number" id="turntable-minute" value="0" min="0" max="59" style="width:60px;">分
<input type="number" id="turntable-second" value="0" min="0" max="59" style="width:60px;">秒
<button class="action-btn btn-primary" id="turntable-btn" onclick="startTurntable()">开始抽奖</button>
<button class="action-btn btn-warning" onclick="retryTurntableFailed()">重新运行失败账号</button>
<button class="action-btn btn-info" onclick="retryTurntableLogin()">重新登录失败账号</button>
<button class="action-btn btn-danger" onclick="stopTask()">停止执行</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
</div>
<div id="turntable-status" class="status-bar status-idle">准备就绪</div>
<div class="stats">
<div class="stat-item"><div class="number" id="turntable-total">0</div><div class="label">账号总数</div></div>
<div class="stat-item"><div class="number" id="turntable-success">0</div><div class="label">成功</div></div>
<div class="stat-item"><div class="number" id="turntable-fail">0</div><div class="label">失败</div></div>
</div>
<div id="turntable-results"></div>
</div>
<div id="coupon" class="tab-content">
<div class="info-card"><strong>查询优惠券说明：</strong>查询所有账号的优惠券信息，包括优惠券标题、数量、币种和ID。</div>
<div class="control-panel">
<label>并发线程数：</label><input type="number" id="coupon-concurrency" value="8" min="1" max="100">
<label><input type="checkbox" id="coupon-use-proxy" checked>使用代理</label>
<button class="action-btn btn-primary" id="coupon-btn" onclick="startCouponQuery()">开始查询</button>
<button class="action-btn btn-danger" onclick="stopTask()">停止执行</button>
<button class="action-btn btn-secondary" onclick="retryCouponFailed()">重新运行失败账号</button>
<button class="action-btn btn-secondary" onclick="retryCouponLogin()">重新登录失败账号</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
</div>
<div id="coupon-status" class="status-bar status-idle">准备就绪</div>
<div id="coupon-results"></div>
</div>
<div id="grid" class="tab-content">
<div class="info-card"><strong>现货网格/马丁格尔下单说明：</strong>使用优惠券下单现货网格或马丁格尔策略，自动获取优惠券ID、下单、查询策略状态。</div>
<div class="control-panel">
<label>并发线程数：</label><input type="number" id="grid-concurrency" value="8" min="1" max="100">
<label>延迟(秒)：</label><input type="number" id="grid-delay" value="0" min="0" max="60">
<label><input type="checkbox" id="grid-use-proxy" checked>使用代理</label>
<label><input type="checkbox" id="grid-martingale">马丁格尔</label>
<button class="action-btn btn-primary" id="grid-btn" onclick="startGridOrder()">开始下单</button>
<button class="action-btn btn-danger" onclick="stopTask()">停止执行</button>
<button class="action-btn btn-secondary" onclick="retryGridFailed()">重新运行失败账号</button>
<button class="action-btn btn-secondary" onclick="retryGridLogin()">重新登录失败账号</button>
<button class="action-btn btn-primary" onclick="queryGridProfit()">查询收益</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
<button class="action-btn btn-secondary" onclick="extractUnusedTokens()" style="background:#28a745;color:white;">提取未下单Token</button>
<button class="sort-btn" id="sort-btn" onclick="toggleSort()">按金额排序</button>
</div>
<div id="grid-status" class="status-bar status-idle">准备就绪</div>
<div id="grid-results"></div>
</div>
<div id="asset" class="tab-content">
<div class="info-card"><strong>查询资产说明：</strong>查询所有账号的USDT总资产余额。</div>
<div class="control-panel">
<label>并发线程数：</label><input type="number" id="asset-concurrency" value="8" min="1" max="100">
<label>延迟(秒)：</label><input type="number" id="asset-delay" value="0" min="0" max="60">
<label><input type="checkbox" id="asset-use-proxy" checked>使用代理</label>
<button class="action-btn btn-primary" id="asset-btn" onclick="startAssetQuery()">查询资产</button>
<button class="action-btn btn-danger" onclick="stopTask()">停止执行</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
<button class="sort-btn" id="sort-btn" onclick="toggleSort()">按金额排序</button>
</div>
<div id="asset-status" class="status-bar status-idle">准备就绪</div>
<div class="stats">
<div class="stat-item"><div class="number" id="asset-total">0</div><div class="label">账号总数</div></div>
<div class="stat-item"><div class="number" id="asset-success">0</div><div class="label">成功</div></div>
<div class="stat-item"><div class="number" id="asset-fail">0</div><div class="label">失败</div></div>
</div>
<div id="asset-results"></div>
</div>
<div id="return" class="tab-content">
<div class="info-card"><strong>回归奖励说明：</strong>自动查询并领取所有账号的未领取回归奖励，支持显示USDT和徽章奖励。</div>
<div class="control-panel">
<label>并发线程数：</label><input type="number" id="return-concurrency" value="8" min="1" max="100">
<button class="action-btn btn-primary" id="return-btn" onclick="startReturnReward()">开始领取</button>
<button class="action-btn btn-secondary" onclick="clearResults()">清空结果</button>
</div>
<div id="return-status" class="status-bar status-idle">准备就绪</div>
<div class="stats">
<div class="stat-item"><div class="number" id="return-total">0</div><div class="label">账号总数</div></div>
<div class="stat-item"><div class="number" id="return-success">0</div><div class="label">成功</div></div>
<div class="stat-item"><div class="number" id="return-fail">0</div><div class="label">失败</div></div>
</div>
<div id="return-results"></div>
</div>
<div id="probe" class="tab-content">
<div class="info-card"><strong>代理测速说明：</strong>并发测试代理列表中每个代理的 RTT，按阈值筛选通过的代理，可一键填入签到代理列表或复制到剪贴板。</div>
<div class="control-panel">
<label>RTT上限(ms)：</label><input type="number" id="probe-threshold" value="2000" min="1">
<label>并发数：</label><input type="number" id="probe-concurrency" value="20" min="1" max="100">
<label>每代理测几次：</label><input type="number" id="probe-times" value="2" min="1" max="10">
<label>目标URL：</label><input type="text" id="probe-target" value="https://www.htx.net.im/" style="width:280px;">
<button class="action-btn btn-primary" id="probe-btn" onclick="startProxyProbe()">开始测速</button>
<button class="action-btn btn-danger" onclick="stopProxyProbe()">停止</button>
<button class="action-btn btn-success" onclick="applyProbeToSignIn()">填入签到代理列表</button>
<button class="action-btn btn-info" onclick="copyProbePassed()">复制通过的代理</button>
<button class="action-btn btn-secondary" onclick="clearProbeResults()">清空结果</button>
</div>
<div class="control-panel">
<label>代理列表（每行一个，支持 http://ip:port / http://user:pass@ip:port / ip:port:user:pass）：</label>
<textarea id="probe-proxy-list" rows="8" style="width:100%;min-width:400px;font-family:monospace;" placeholder="http://127.0.0.1:21001&#10;http://127.0.0.1:21002"></textarea>
</div>
<div id="probe-status" class="status-bar status-idle">准备就绪</div>
<div class="stats">
<div class="stat-item"><div class="number" id="probe-total">0</div><div class="label">总数</div></div>
<div class="stat-item"><div class="number" id="probe-pass">0</div><div class="label">通过</div></div>
<div class="stat-item"><div class="number" id="probe-fail">0</div><div class="label">失败</div></div>
</div>
<div id="probe-results"></div>
</div>
</div>
<div class="global-status-bar">
<div id="global-status-text">就绪</div>
<div id="global-progress">
<div id="global-progress-bar"></div>
</div>
<div id="global-stats">已处理: <span id="global-processed">0</span> / <span id="global-total">0</span></div>
</div>
</div>
<script>
var currentTab = 'login';
var refreshInterval = null;
var currentSort = '';
var currentSortDir = 'desc';
function isSortableTab(tab){return tab==='asset' || tab==='grid';}
function updateSortButton(){var sortBtn=document.getElementById('sort-btn');if(!sortBtn)return;if(isSortableTab(currentTab)){sortBtn.style.display='inline-block';if(currentSort==='amount'){sortBtn.textContent=currentSortDir==='desc'?'按金额 ↓':'按金额 ↑';sortBtn.classList.add('active-sort');}else{sortBtn.textContent='按金额排序';sortBtn.classList.remove('active-sort');}}else{sortBtn.style.display='none';currentSort='';}}
function toggleSort(){if(!isSortableTab(currentTab))return;if(currentSort===''){currentSort='amount';currentSortDir='desc';}else if(currentSort==='amount'&&currentSortDir==='desc'){currentSortDir='asc';}else{currentSort='';currentSortDir='desc';}updateSortButton();loadResults();}
var countdownInterval = null;
function switchTab(tab) {
document.querySelectorAll('.tab-btn').forEach(function(btn){btn.classList.remove('active');});
document.querySelectorAll('.tab-content').forEach(function(content){content.classList.remove('active');});
document.querySelector('.tab-btn[data-tab=\"'+tab+'\"]').classList.add('active');
document.getElementById(tab).classList.add('active');
currentTab = tab;
loadAccounts();
updateSortButton();
if(refreshInterval) clearInterval(refreshInterval);
if(countdownInterval) clearInterval(countdownInterval);
if(tab === 'exchange'){
countdownInterval = setInterval(updateExchangeCountdown, 1000);
updateExchangeCountdown();
}
refreshInterval = setInterval(function(){checkStatus();loadResults();},2000);
}
function updateExchangeCountdown() {
var h = parseInt(document.getElementById('exchange-hour').value)||0;
var m = parseInt(document.getElementById('exchange-minute').value)||0;
var s = parseInt(document.getElementById('exchange-second').value)||0;
var now = new Date();
var target = new Date(now.getFullYear(),now.getMonth(),now.getDate(),h,m,s);
if(target < now) target.setDate(target.getDate()+1);
var diff = target - now;
if(diff > 0){
var hours = Math.floor(diff/3600000);
var minutes = Math.floor((diff%3600000)/60000);
var seconds = Math.floor((diff%60000)/1000);
document.getElementById('exchange-countdown').innerHTML = '距离目标时间: <span style=\"color:#e74c3c;\">'+
String(hours).padStart(2,'0')+':'+String(minutes).padStart(2,'0')+':'+String(seconds).padStart(2,'0')+'</span>';
}else{
document.getElementById('exchange-countdown').innerHTML = '目标时间已到，可立即执行';
}
}
function loadAccounts() {
fetch('/api/accounts').then(function(res){return res.json();}).then(function(data){
document.getElementById(currentTab+'-total').textContent = data.count;
}).catch(function(err){console.error(err);});
}
function checkStatus() {
fetch('/api/status').then(function(res){return res.json();}).then(function(data){
var statusBar = document.getElementById(currentTab+'-status');
var btn = document.getElementById(currentTab+'-btn');
statusBar.textContent = data.message;
btn.disabled = data.running;
var cls = 'status-idle';
if(data.running){cls='status-running';}
else if(data.message.indexOf('完毕')>=0){cls='status-done';}
statusBar.className = 'status-bar '+cls;

document.getElementById('global-status-text').textContent = data.message;
document.getElementById('global-processed').textContent = data.processed;
document.getElementById('global-total').textContent = data.total;
var progress = 0;
if(data.total > 0){progress = (data.processed / data.total) * 100;}
document.getElementById('global-progress-bar').style.width = progress+'%';
}).catch(function(err){console.error(err);});
}
function loadResults() {
if (currentTab === 'probe') {
if (probeRunning) {
fetch('/api/probe/results').then(function(res){return res.json();}).then(function(data){
probeResults = data.results || [];
renderProbeResults();
if (data.done) {
probeRunning = false;
document.getElementById('probe-btn').disabled = false;
document.getElementById('probe-status').className = 'status-bar status-ok';
document.getElementById('probe-status').textContent = data.message || '测速完成';
}
}).catch(function(err){console.error(err);});
}
return;
}
var url='/api/results?tab='+currentTab;
if(currentSort==='amount'){url+='&sort=amount&dir='+currentSortDir;}
fetch(url).then(function(res){return res.json();}).then(function(data){renderResults(data.results);}).catch(function(err){console.error(err);});
}
function renderResults(results) {
var container = document.getElementById(currentTab+'-results');
var success = 0, fail = 0;
if(results.length===0){
container.innerHTML = '<p style=\"color:#999;text-align:center;padding:20px;\">暂无结果</p>';
}else{
var html = '<table class=\"results-table\"><thead><tr>';
if(currentTab==='login'){
html += '<th>ID</th><th>邮箱</th><th>VToken</th><th>ProToken</th><th>UCToken</th><th>状态</th><th>结果</th>';
}else if(currentTab==='signin'){
html += '<th>账号</th><th>状态</th><th>福利金</th><th>签到前勋章数量</th><th>福利签到状态</th><th>邀请签到状态</th><th>徽章数量</th><th>操作时间</th>';
}else if(currentTab==='return'){
html += '<th>邮箱</th><th>结果</th><th>USDT奖励</th><th>徽章奖励</th><th>时间</th>';
}else if(currentTab==='grid'){
html += '<th>邮箱</th><th>状态</th><th>优惠券ID</th><th>策略单号</th><th>盈利</th><th>代理IP</th><th>详情</th><th>时间</th>';
}else{
html += '<th>邮箱</th><th>结果</th><th>时间</th>';
}
html += '</tr></thead><tbody>';
for(var i=0;i<results.length;i++){
var item = results[i];
var isSuccess = false;
if(currentTab==='signin'){
var signInSuccess = item.status!=='失败' && item.status!=='错误';
if(signInSuccess) success++; else fail++;
html += '<tr><td>'+item.email+'</td><td><span class=\"'+(signInSuccess?'badge-success':'badge-error')+'\">'+(item.status||'-')+'</span></td><td>'+(item.welfare_gold||'-')+'</td><td>'+(item.badge_before||'-')+'</td><td>'+(item.welfare_sign_status||'-')+'</td><td>'+(item.invite_sign_status||'-')+'</td><td>'+(item.badge_count||'-')+'</td><td>'+(item.operation_time||'-')+'</td></tr>';
continue;
}
if(currentTab==='grid'){
isSuccess = item.result && item.result.indexOf('失败')<0 && item.result.indexOf('错误')<0;
if(isSuccess) success++; else fail++;
html += '<tr><td>'+item.email+'</td><td><span class=\"'+(isSuccess?'badge-success':'badge-error')+'\">'+item.result+'</span></td><td>'+(item.coupon_id || '-')+'</td><td>'+(item.strategy_id || '-')+'</td><td style=\"color:'+(item.profit && item.profit!=='' && item.profit!=='创建策略成功' ? '#28a745' : '#666')+'\">'+(item.profit || '-')+'</td><td>'+(item.ip || '-')+'</td><td>'+(item.detail || '-')+'</td><td>'+item.timestamp+'</td></tr>';
continue;
}
isSuccess = item.result && item.result.indexOf('失败')<0 && item.result.indexOf('错误')<0 && item.result!='代理获取失败';
if(currentTab==='login'){
isSuccess = item.result==='登录成功' || item.status==='完成';
}
if(isSuccess) success++; else fail++;
if(currentTab==='login'){
html += '<tr><td>'+item.id+'</td><td>'+item.email+'</td><td>'+(item.vtoken||'-')+'</td><td>'+(item.protoken||'-')+'</td><td>'+(item.uctoken||'-')+'</td><td><span class=\"'+(isSuccess?'badge-success':'badge-error')+'\">'+item.status+'</span></td><td>'+item.result+'</td></tr>';
}else if(currentTab==='return'){
html += '<tr><td>'+item.email+'</td><td><span class=\"'+(isSuccess?'badge-success':'badge-error')+'\">'+item.result+'</span></td><td>'+(item.usdt || '-')+'</td><td>'+(item.badge || '-')+'</td><td>'+item.timestamp+'</td></tr>';
}else{
html += '<tr><td>'+item.email+'</td><td><span class=\"'+(isSuccess?'badge-success':'badge-error')+'\">'+item.result+'</span></td><td>'+item.timestamp+'</td></tr>';
}
}
html += '</tbody></table>';
container.innerHTML = html;
}
document.getElementById(currentTab+'-success').textContent = success;
document.getElementById(currentTab+'-fail').textContent = fail;
}
function startLogin() {
var concurrency = document.getElementById('login-concurrency').value;
var proxyFetchURL = document.getElementById('login-proxy-fetch-url').value;
var useProxy = document.getElementById('login-use-proxy').checked;
var proxyURL = document.getElementById('login-proxy-url').value;
fetch('/api/login',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+concurrency+'&proxy_fetch_url='+encodeURIComponent(proxyFetchURL)+'&use_proxy='+useProxy+'&proxy_url='+encodeURIComponent(proxyURL)})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function startExchange() {
var h = document.getElementById('exchange-hour').value;
var m = document.getElementById('exchange-minute').value;
var s = document.getElementById('exchange-second').value;
var retry = document.getElementById('exchange-retry').value;
var useProxy = document.getElementById('exchange-use-proxy').checked ? 1 : 0;
var advanceMs = document.getElementById('exchange-advance-ms').value;
var useServerTime = document.getElementById('exchange-use-server-time').checked ? 1 : 0;
fetch('/api/exchange',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'hour='+h+'&minute='+m+'&second='+s+'&retry_count='+retry+'&use_proxy='+useProxy+'&advance_ms='+advanceMs+'&use_server_time='+useServerTime})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function startSignIn() {
var useProxy = document.getElementById('signin-use-proxy').checked ? 1 : 0;
fetch('/api/signin',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('signin-concurrency').value+'&use_proxy='+useProxy})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function retryFailedSignIn() {
var useProxy = document.getElementById('signin-use-proxy').checked ? 1 : 0;
fetch('/api/signin/retry',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('signin-concurrency').value+'&use_proxy='+useProxy})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function retryLoginFailed() {
fetch('/api/signin/retry-login',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('signin-concurrency').value})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function sortSignInByWelfare() {
fetch('/api/signin/sort-welfare',{method:'POST'})
.then(function(res){return res.json();}).then(function(r){
var el=document.getElementById('signin-status');
if(r && r.message) { el.className='status-bar status-ok'; el.textContent=r.message; }
renderResults('signin');
});
}
function saveSignInOrder() {
fetch('/api/signin/save-order',{method:'POST'})
.then(function(res){return res.json();}).then(function(r){
alert(r && r.message ? r.message : '保存完成');
});
}
var probeResults = [];
var probeRunning = false;
function startProxyProbe() {
var proxies = document.getElementById('probe-proxy-list').value.split('\n').map(function(s){return s.trim();}).filter(function(s){return s && s.indexOf('#')!==0;});
if (proxies.length === 0) { alert('请先填入代理列表'); return; }
var body = 'proxies=' + encodeURIComponent(proxies.join('\n')) +
'&threshold=' + document.getElementById('probe-threshold').value +
'&concurrency=' + document.getElementById('probe-concurrency').value +
'&times=' + document.getElementById('probe-times').value +
'&target=' + encodeURIComponent(document.getElementById('probe-target').value);
probeRunning = true;
document.getElementById('probe-btn').disabled = true;
document.getElementById('probe-status').className = 'status-bar status-running';
document.getElementById('probe-status').textContent = '测速中...';
fetch('/api/probe/start', {method:'POST', headers:{'Content-Type':'application/x-www-form-urlencoded'}, body: body})
.then(function(res){return res.json();}).then(function(r){
if (r.status === 'error') {
probeRunning = false;
document.getElementById('probe-btn').disabled = false;
document.getElementById('probe-status').className = 'status-bar status-error';
document.getElementById('probe-status').textContent = r.message || '启动失败';
}
});
}
function stopProxyProbe() {
fetch('/api/probe/stop',{method:'POST'}).then(function(res){return res.json();}).then(function(){});
}
function renderProbeResults() {
var html = '<table class=\"results-table\"><thead><tr><th>代理</th><th>RTT</th><th>状态</th><th>通过</th></tr></thead><tbody>';
var passCount = 0, failCount = 0;
for (var i = 0; i < probeResults.length; i++) {
var r = probeResults[i];
if (r.pass) passCount++; else failCount++;
html += '<tr><td>' + r.proxy + '</td><td>' + (r.rtt_ms < 0 ? '-' : r.rtt_ms + 'ms') + '</td><td>' + (r.status || '-') + '</td><td>' + (r.pass ? '✓' : '✗') + '</td></tr>';
}
html += '</tbody></table>';
document.getElementById('probe-results').innerHTML = html;
document.getElementById('probe-total').textContent = probeResults.length;
document.getElementById('probe-pass').textContent = passCount;
document.getElementById('probe-fail').textContent = failCount;
}
function applyProbeToSignIn() {
var passed = probeResults.filter(function(r){return r.pass;}).map(function(r){return r.proxy;});
if (passed.length === 0) { alert('没有通过的代理，请先测速'); return; }
// 按 RTT 升序
passed.sort(function(a, b){
var ra = probeResults.find(function(r){return r.proxy === a;});
var rb = probeResults.find(function(r){return r.proxy === b;});
return ra.rtt_ms - rb.rtt_ms;
});
fetch('/api/signin/proxy-list', {method:'POST', headers:{'Content-Type':'application/x-www-form-urlencoded'}, body:'proxy_list=' + encodeURIComponent(passed.join('\n'))})
.then(function(res){return res.json();}).then(function(r){
alert(r.message || '已填入');
});
}
function copyProbePassed() {
var passed = probeResults.filter(function(r){return r.pass;}).map(function(r){return r.proxy;});
if (passed.length === 0) { alert('没有通过的代理'); return; }
navigator.clipboard.writeText(passed.join('\n')).then(function(){
alert('已复制 ' + passed.length + ' 个代理');
});
}
function clearProbeResults() {
probeResults = [];
renderProbeResults();
document.getElementById('probe-status').className = 'status-bar status-idle';
document.getElementById('probe-status').textContent = '准备就绪';
}
function startHongBao() {
var useSchedule = document.getElementById('hongbao-use-schedule').checked;
var hour = document.getElementById('hongbao-hour').value;
var minute = document.getElementById('hongbao-minute').value;
var second = document.getElementById('hongbao-second').value;
fetch('/api/hongbao',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('hongbao-concurrency').value+'&use_schedule='+useSchedule+'&hour='+hour+'&minute='+minute+'&second='+second})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function stopTask() {
fetch('/api/stop',{method:'POST'}).then(function(res){return res.json();}).then(function(){checkStatus();});
}
function startReturnReward() {
fetch('/api/return',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('return-concurrency').value})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function startTurntable() {
var activityId = document.getElementById('turntable-activity-id').value;
var useSchedule = document.getElementById('turntable-use-schedule').checked;
var hour = document.getElementById('turntable-hour').value;
var minute = document.getElementById('turntable-minute').value;
var second = document.getElementById('turntable-second').value;
fetch('/api/turntable',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'activity_id='+encodeURIComponent(activityId)+'&concurrency='+document.getElementById('turntable-concurrency').value+'&use_schedule='+useSchedule+'&hour='+hour+'&minute='+minute+'&second='+second})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function retryTurntableFailed() {
var activityId = document.getElementById('turntable-activity-id').value;
fetch('/api/turntable-retry-failed',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'activity_id='+encodeURIComponent(activityId)+'&concurrency='+document.getElementById('turntable-concurrency').value})
.then(function(res){return res.json();});
}
function retryTurntableLogin() {
fetch('/api/turntable-retry-login',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('turntable-concurrency').value})
.then(function(res){return res.json();});
}
function startCouponQuery() {
var useProxy = document.getElementById('coupon-use-proxy').checked;
fetch('/api/coupon',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('coupon-concurrency').value+'&use_proxy='+useProxy})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function retryCouponFailed() {
var useProxy = document.getElementById('coupon-use-proxy').checked;
fetch('/api/coupon-retry-failed',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('coupon-concurrency').value+'&use_proxy='+useProxy})
.then(function(res){return res.json();});
}
function retryCouponLogin() {
var useProxy = document.getElementById('coupon-use-proxy').checked;
fetch('/api/coupon-retry-login',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('coupon-concurrency').value+'&use_proxy='+useProxy})
.then(function(res){return res.json();});
}
function startGridOrder() {
var useProxy = document.getElementById('grid-use-proxy').checked;
var martingale = document.getElementById('grid-martingale').checked;
var strategyType = martingale ? 1 : 0;
var delay = document.getElementById('grid-delay').value || 0;
fetch('/api/grid-order',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('grid-concurrency').value+'&use_proxy='+useProxy+'&delay='+delay+'&strategy_type='+strategyType})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function retryGridFailed() {
var useProxy = document.getElementById('grid-use-proxy').checked;
var martingale = document.getElementById('grid-martingale').checked;
var strategyType = martingale ? 1 : 0;
var delay = document.getElementById('grid-delay').value || 0;
fetch('/api/grid-order-retry-failed',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('grid-concurrency').value+'&use_proxy='+useProxy+'&delay='+delay+'&strategy_type='+strategyType})
.then(function(res){return res.json();});
}
function retryGridLogin() {
fetch('/api/grid-order-retry-login',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('grid-concurrency').value})
.then(function(res){return res.json();});
}
function queryGridProfit() {
var useProxy = document.getElementById('grid-use-proxy').checked;
var martingale = document.getElementById('grid-martingale').checked;
var strategyType = martingale ? 1 : 0;
var delay = document.getElementById('grid-delay').value || 0;
fetch('/api/grid-order-profit',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('grid-concurrency').value+'&use_proxy='+useProxy+'&delay='+delay+'&strategy_type='+strategyType})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function startAssetQuery() {
var useProxy = document.getElementById('asset-use-proxy').checked;
var delay = document.getElementById('asset-delay').value || 0;
fetch('/api/asset-query',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'concurrency='+document.getElementById('asset-concurrency').value+'&use_proxy='+useProxy+'&delay='+delay})
.then(function(res){return res.json();}).then(function(){loadAccounts();});
}
function clearResults() {
fetch('/api/clear',{method:'POST'}).then(function(res){return res.json();}).then(function(){loadResults();checkStatus();});
}
function extractAccounts() {
fetch('/api/extract-accounts',{method:'POST'}).then(function(res){return res.json();}).then(function(data){
alert(data.message);
});
}
function extractUnusedTokens() {
if(!confirm('确定要提取没有下单的Token吗？')) return;
fetch('/api/extract-unused-tokens',{method:'POST'}).then(function(res){return res.json();}).then(function(data){
if(data.success){
alert('正在提取未下单Token，请查看状态...');
}else{
alert('提取失败: '+data.message);
}
});
}
function switchTokenFile() {
var path = document.getElementById('token-file-path').value;
if(!path){
alert('请输入文件路径');
return;
}
fetch('/api/switch-token-file',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:path})}).then(function(res){return res.json();}).then(function(data){
alert(data.message);
});
}
function useDefaultTokenFile() {
fetch('/api/use-default-token-file',{method:'POST'}).then(function(res){return res.json();}).then(function(data){
alert(data.message);
document.getElementById('token-file-path').value = '';
});
}
document.getElementById('login-use-proxy').addEventListener('change',function(e){
document.getElementById('login-proxy-url').disabled = !e.target.checked;
});
window.addEventListener('load',function(){
loadAccounts();
updateSortButton();
refreshInterval = setInterval(function(){checkStatus();loadResults();},2000);
});
</script>
</body>
</html>`
}

// parseWelfareGoldWeb 解析福利金字符串为数值，解析失败返回 0
func parseWelfareGoldWeb(s string) float64 {
	if s == "" || s == "-" {
		return 0
	}
	s = strings.ReplaceAll(s, ",", "")
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return 0
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	port := "39271"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}

	// Web 启动时不预设代理，由用户在页面填写后每次请求时应用

	r.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(getIndexHTML()))
	})

	r.GET("/api/status", func(c *gin.Context) {
		statusMu.Lock()
		defer statusMu.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"running":   taskStatus.Running,
			"message":   taskStatus.Message,
			"processed": taskStatus.Processed,
			"total":     taskStatus.Total,
		})
	})

	r.GET("/api/accounts", func(c *gin.Context) {
		tokens, err := LoadTokens()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		accounts := make([]string, 0, len(tokens))
		for email := range tokens {
			accounts = append(accounts, email)
		}
		c.JSON(http.StatusOK, gin.H{"count": len(tokens), "accounts": accounts})
	})

	r.GET("/api/results", func(c *gin.Context) {
		tab := c.DefaultQuery("tab", "login")
		sortBy := c.DefaultQuery("sort", "")     // "amount" or ""
		sortDir := c.DefaultQuery("dir", "desc") // "desc" or "asc"

		if tab == "login" {
			c.JSON(http.StatusOK, gin.H{"results": loginResults})
		} else if tab == "signin" {
			c.JSON(http.StatusOK, gin.H{"results": signInResults})
		} else {
			results := resultsMap[tab]

			// 根据金额排序
			if sortBy == "amount" {
				sort.Slice(results, func(i, j int) bool {
					if sortDir == "asc" {
						return results[i].SortValue < results[j].SortValue
					}
					return results[i].SortValue > results[j].SortValue
				})
			}

			c.JSON(http.StatusOK, gin.H{"results": results})
		}
	})

	r.POST("/api/login", func(c *gin.Context) {
		var req LoginRequest
		c.Bind(&req)
		if req.Concurrency <= 0 {
			req.Concurrency = 200
		}
		go runLoginWeb(req.Concurrency, req.ProxyFetchURL, req.UseProxy, req.ProxyURL)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/exchange", func(c *gin.Context) {
		var req ExchangeRequest
		c.Bind(&req)
		if req.RetryCount <= 0 {
			req.RetryCount = 10
		}
		go runExchangeWeb(req.Hour, req.Minute, req.Second, req.RetryCount, req.UseProxy, req.AdvanceMs, req.UseServerTime)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/signin", func(c *gin.Context) {
		var req TaskRequest
		c.Bind(&req)

		if req.Concurrency <= 0 {
			req.Concurrency = 10
		}
		go runSignInWeb(req.Concurrency, req.UseProxy)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/signin/retry", func(c *gin.Context) {
		var req TaskRequest
		c.Bind(&req)
		if req.Concurrency <= 0 {
			req.Concurrency = 10
		}
		go runRetryFailedSignIn(req.Concurrency, req.UseProxy)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/signin/retry-login", func(c *gin.Context) {
		var req TaskRequest
		c.Bind(&req)
		if req.Concurrency <= 0 {
			req.Concurrency = 10
		}
		go runRetryLoginFailed(req.Concurrency)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	// 按福利金降序排序签到结果
	r.POST("/api/signin/sort-welfare", func(c *gin.Context) {
		signInResultsMu.Lock()
		defer signInResultsMu.Unlock()
		if len(signInResults) == 0 {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "暂无签到结果，请先签到"})
			return
		}
		sort.SliceStable(signInResults, func(i, j int) bool {
			return parseWelfareGoldWeb(signInResults[i].WelfareGold) > parseWelfareGoldWeb(signInResults[j].WelfareGold)
		})
		count := 0
		var total float64
		for _, r := range signInResults {
			g := parseWelfareGoldWeb(r.WelfareGold)
			if g > 0 {
				count++
				total += g
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("已按福利金降序排序（有值:%d个, 合计:%.2f）", count, total),
		})
	})

	// 按当前显示顺序保存 tokens.json
	r.POST("/api/signin/save-order", func(c *gin.Context) {
		signInResultsMu.Lock()
		order := make([]string, 0, len(signInResults))
		for _, r := range signInResults {
			if r.Email != "" {
				order = append(order, r.Email)
			}
		}
		signInResultsMu.Unlock()

		if len(order) == 0 {
			tokens, err := LoadTokens()
			if err != nil || len(tokens) == 0 {
				c.JSON(http.StatusOK, gin.H{"success": false, "message": "无可保存的账号"})
				return
			}
			for e := range tokens {
				order = append(order, e)
			}
		}
		tokens, err := LoadTokens()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		if err := SaveTokensOrdered(tokens, order); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("✓ 已按当前顺序保存 %d 个账号到 tokens.json", len(order)),
		})
	})

	type HongBaoRequest struct {
		Concurrency int  `json:"concurrency" form:"concurrency"`
		UseSchedule bool `json:"use_schedule" form:"use_schedule"`
		Hour        int  `json:"hour" form:"hour"`
		Minute      int  `json:"minute" form:"minute"`
		Second      int  `json:"second" form:"second"`
	}

	r.POST("/api/hongbao", func(c *gin.Context) {
		var req HongBaoRequest
		c.Bind(&req)

		if req.Concurrency <= 0 {
			req.Concurrency = 8
		}
		go runHongBaoWeb(req.Concurrency, req.UseSchedule, req.Hour, req.Minute, req.Second)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/stop", func(c *gin.Context) {
		statusMu.Lock()
		if stopChan != nil {
			close(stopChan)
			stopChan = nil
		}
		statusMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"status": "stopped"})
	})

	// ==================== 代理测速 ====================
	type ProbeResult struct {
		Proxy  string `json:"proxy"`
		RTTMs  int64  `json:"rtt_ms"`
		Status string `json:"status"`
		Pass   bool   `json:"pass"`
	}
	type ProbeState struct {
		Results []ProbeResult
		Done    bool
		Message string
		Running bool
	}
	var (
		probeStateMu sync.Mutex
		probeState   = ProbeState{}
		probeStopCh  chan struct{}
	)

	r.POST("/api/probe/start", func(c *gin.Context) {
		proxiesStr := c.PostForm("proxies")
		thresholdStr := c.DefaultPostForm("threshold", "2000")
		concurrencyStr := c.DefaultPostForm("concurrency", "20")
		timesStr := c.DefaultPostForm("times", "2")
		target := c.DefaultPostForm("target", "https://www.htx.net.im/")

		var proxies []string
		for _, line := range strings.Split(proxiesStr, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			proxies = append(proxies, line)
		}
		if len(proxies) == 0 {
			c.JSON(http.StatusOK, gin.H{"status": "error", "message": "未提供有效代理"})
			return
		}
		threshold, err := strconv.Atoi(strings.TrimSpace(thresholdStr))
		if err != nil || threshold <= 0 {
			threshold = 2000
		}
		concurrency, err := strconv.Atoi(strings.TrimSpace(concurrencyStr))
		if err != nil || concurrency <= 0 {
			concurrency = 20
		}
		times, err := strconv.Atoi(strings.TrimSpace(timesStr))
		if err != nil || times <= 0 {
			times = 2
		}

		probeStateMu.Lock()
		if probeState.Running {
			c.JSON(http.StatusOK, gin.H{"status": "error", "message": "已有测速任务在运行"})
			probeStateMu.Unlock()
			return
		}
		probeState = ProbeState{
			Results: make([]ProbeResult, len(proxies)),
			Done:    false,
			Running: true,
		}
		for i, p := range proxies {
			probeState.Results[i] = ProbeResult{Proxy: p, RTTMs: -1, Status: "等待中"}
		}
		probeStopCh = make(chan struct{})
		probeStateMu.Unlock()

		log.Printf("[代理测速] 开始: %d 个, 阈值=%dms, 并发=%d, 目标=%s", len(proxies), threshold, concurrency, target)
		go func(proxies []string, threshold, concurrency, times int, target string) {
			sem := make(chan struct{}, concurrency)
			var wg sync.WaitGroup
			var passCount, failCount int64

			for i, p := range proxies {
				select {
				case <-probeStopCh:
					break
				default:
				}
				wg.Add(1)
				sem <- struct{}{}
				go func(idx int, proxy string) {
					defer wg.Done()
					defer func() { <-sem }()

					var minRTT time.Duration
					var lastErr string
					for t := 0; t < times; t++ {
						select {
						case <-probeStopCh:
							return
						default:
						}
						rtt, err := probeProxyOnce(proxy, target)
						if err != nil {
							lastErr = err.Error()
							continue
						}
						lastErr = ""
						if minRTT == 0 || rtt < minRTT {
							minRTT = rtt
						}
					}

					res := ProbeResult{Proxy: proxy}
					if lastErr != "" && minRTT == 0 {
						res.RTTMs = -1
						res.Status = "失败: " + lastErr
						res.Pass = false
						atomic.AddInt64(&failCount, 1)
					} else {
						res.RTTMs = minRTT.Milliseconds()
						if minRTT <= time.Duration(threshold)*time.Millisecond {
							res.Pass = true
							res.Status = "通过"
							atomic.AddInt64(&passCount, 1)
						} else {
							res.Pass = false
							res.Status = fmt.Sprintf("超阈值(%dms)", threshold)
							atomic.AddInt64(&failCount, 1)
						}
					}

					probeStateMu.Lock()
					if idx < len(probeState.Results) {
						probeState.Results[idx] = res
					}
					probeStateMu.Unlock()
				}(i, p)
			}

			wg.Wait()
			probeStateMu.Lock()
			probeState.Done = true
			probeState.Running = false
			probeState.Message = fmt.Sprintf("测速完成: 通过 %d / 失败 %d / 共 %d", atomic.LoadInt64(&passCount), atomic.LoadInt64(&failCount), len(proxies))
			probeStateMu.Unlock()
			log.Printf("[代理测速] %s", probeState.Message)
		}(proxies, threshold, concurrency, times, target)

		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/probe/stop", func(c *gin.Context) {
		probeStateMu.Lock()
		if probeStopCh != nil {
			select {
			case <-probeStopCh:
			default:
				close(probeStopCh)
			}
		}
		probeStateMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"status": "stopped"})
	})

	r.GET("/api/probe/results", func(c *gin.Context) {
		probeStateMu.Lock()
		defer probeStateMu.Unlock()
		// 复制一份避免竞争
		resultsCopy := make([]ProbeResult, len(probeState.Results))
		copy(resultsCopy, probeState.Results)
		c.JSON(http.StatusOK, gin.H{
			"results": resultsCopy,
			"done":    probeState.Done,
			"message": probeState.Message,
		})
	})

	// 把指定代理列表写入签到代理列表（由前端"填入签到代理列表"按钮调用）
	r.POST("/api/signin/proxy-list", func(c *gin.Context) {
		proxyList := c.PostForm("proxy_list")
		// 保存到独立文件，签到 tab 启动时自动加载
		if err := os.WriteFile("./data/signin_proxy_list.txt", []byte(proxyList), 0644); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "保存失败: " + err.Error()})
			return
		}
		lines := len(strings.Split(proxyList, "\n"))
		c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("已将 %d 个代理填入签到代理列表（已保存到 data/signin_proxy_list.txt）", lines)})
	})

	// 获取签到代理列表（签到 tab 启动时加载）
	r.GET("/api/signin/proxy-list", func(c *gin.Context) {
		data, err := os.ReadFile("./data/signin_proxy_list.txt")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"proxy_list": ""})
			return
		}
		c.JSON(http.StatusOK, gin.H{"proxy_list": string(data)})
	})

	r.POST("/api/return", func(c *gin.Context) {
		var req TaskRequest
		c.Bind(&req)
		if req.Concurrency <= 0 {
			req.Concurrency = 8
		}
		go runReturnRewardWeb(req.Concurrency)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/turntable", func(c *gin.Context) {
		var req TurntableRequest
		c.Bind(&req)
		go runTurntableWeb(req)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/turntable-retry-failed", func(c *gin.Context) {
		activityId := c.PostForm("activity_id")
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		go runTurntableRetryFailed(activityId, concurrency)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/turntable-retry-login", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		go runTurntableRetryLogin(concurrency)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/coupon", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		useProxy := c.PostForm("use_proxy") == "true"
		go runCouponQuery(concurrency, useProxy)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/coupon-retry-failed", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		useProxy := c.PostForm("use_proxy") == "true"
		go runCouponRetryFailed(concurrency, useProxy)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/coupon-retry-login", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		go runCouponRetryLogin(concurrency)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/grid-order", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		useProxy := c.PostForm("use_proxy") == "true"
		delay, _ := strconv.Atoi(c.PostForm("delay"))
		if delay < 0 {
			delay = 0
		}
		strategyType, _ := strconv.Atoi(c.PostForm("strategy_type"))
		if strategyType < 0 || strategyType > 1 {
			strategyType = 0
		}
		go runGridOrder(concurrency, useProxy, delay, strategyType)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/grid-order-retry-failed", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		useProxy := c.PostForm("use_proxy") == "true"
		delay, _ := strconv.Atoi(c.PostForm("delay"))
		if delay < 0 {
			delay = 0
		}
		strategyType, _ := strconv.Atoi(c.PostForm("strategy_type"))
		if strategyType < 0 || strategyType > 1 {
			strategyType = 0
		}
		go runGridOrderRetryFailed(concurrency, useProxy, delay, strategyType)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/grid-order-retry-login", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		go runGridOrderRetryLogin(concurrency)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/grid-order-profit", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		useProxy := c.PostForm("use_proxy") == "true"
		delay, _ := strconv.Atoi(c.PostForm("delay"))
		if delay < 0 {
			delay = 0
		}
		strategyType, _ := strconv.Atoi(c.PostForm("strategy_type"))
		if strategyType < 0 || strategyType > 1 {
			strategyType = 0
		}
		go runGridProfitQuery(concurrency, useProxy, delay, strategyType)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/asset-query", func(c *gin.Context) {
		concurrency, _ := strconv.Atoi(c.PostForm("concurrency"))
		if concurrency <= 0 {
			concurrency = 8
		}
		useProxy := c.PostForm("use_proxy") == "true"
		delay, _ := strconv.Atoi(c.PostForm("delay"))
		if delay < 0 {
			delay = 0
		}
		go runAssetQuery(concurrency, useProxy, delay)
		c.JSON(http.StatusOK, gin.H{"status": "started"})
	})

	r.POST("/api/clear", func(c *gin.Context) {
		loginResults = []LoginResult{}
		signInResults = []SignInResult{}
		resultsMap["exchange"] = []ResultItem{}
		resultsMap["hongbao"] = []ResultItem{}
		resultsMap["return"] = []ResultItem{}
		resultsMap["turntable"] = []ResultItem{}
		resultsMap["coupon"] = []ResultItem{}
		resultsMap["grid"] = []ResultItem{}
		c.JSON(http.StatusOK, gin.H{"status": "cleared"})
	})

	r.POST("/api/extract-accounts", func(c *gin.Context) {
		count, filename, err := ExtractAccounts()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("提取失败: %v", err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "count": count, "filename": filename, "message": fmt.Sprintf("已提取 %d 个账号到 %s", count, filename)})
	})

	r.POST("/api/switch-token-file", func(c *gin.Context) {
		var req struct {
			Path string `json:"path"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
			return
		}

		if _, err := os.Stat(req.Path); os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "文件不存在"})
			return
		}

		SetTokenFilePath(req.Path)
		c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("已切换到 tokens 文件: %s", req.Path)})
	})

	r.POST("/api/use-default-token-file", func(c *gin.Context) {
		SetTokenFilePath(defaultTokenFile)
		c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("已切换到默认 tokens 文件: %s", defaultTokenFile)})
	})

	r.POST("/api/extract-high-badge", func(c *gin.Context) {
		if !tryStartTask("提取高勋章账号") {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "正在执行其他任务"})
			return
		}
		setStatus(true, "正在查询勋章数量...")

		go func() {
			count, filename, err := ExtractHighBadgeAccounts(100)
			if err != nil {
				setStatus(false, fmt.Sprintf("查询失败: %v", err))
				return
			}
			if count == 0 {
				setStatus(false, "没有找到勋章数量大于100的账号")
				return
			}
			setStatus(false, fmt.Sprintf("已提取 %d 个高勋章账号到 %s", count, filename))
		}()

		c.JSON(http.StatusOK, gin.H{"success": true, "message": "正在查询勋章数量..."})
	})

	r.POST("/api/extract-unused-tokens", func(c *gin.Context) {
		if !tryStartTask("提取未使用Token") {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "正在执行其他任务"})
			return
		}
		setStatus(true, "正在提取未使用Token...")

		go func() {
			count, filename, err := ExtractUnusedTokens()
			if err != nil {
				setStatus(false, fmt.Sprintf("提取失败: %v", err))
				return
			}
			setStatus(false, fmt.Sprintf("已提取 %d 个未使用Token到 %s", count, filename))
		}()

		c.JSON(http.StatusOK, gin.H{"success": true, "message": "正在提取未使用Token..."})
	})

	log.Printf("Web服务启动，监听端口: %s", port)
	log.Printf("访问地址: http://localhost:%s", port)

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

func setStatus(running bool, message string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	taskStatus.Running = running
	taskStatus.Message = message
	if !running {
		taskStatus.Processed = 0
		taskStatus.Total = 0
	}
}

func setProgress(processed, total int) {
	statusMu.Lock()
	defer statusMu.Unlock()
	taskStatus.Processed = processed
	taskStatus.Total = total
}

func incrementProgress() {
	statusMu.Lock()
	defer statusMu.Unlock()
	taskStatus.Processed++
}

func addResult(tab, email, result, usdt, badge string) {
	sortValue := extractSortValue(tab, result, usdt, badge)
	resultsMap[tab] = append(resultsMap[tab], ResultItem{
		Email:     email,
		Result:    result,
		USDT:      usdt,
		Badge:     badge,
		Timestamp: time.Now().Format("15:04:05"),
		SortValue: sortValue,
	})
}

// addGridResult 添加网格订单结果
func addGridResult(email, status, couponID, strategyID, profit, ip, detail string) {
	result := status
	if status == "完成" {
		result = "下单成功"
	}
	sortValue := 0.0
	if profit != "" {
		if f, err := strconv.ParseFloat(strings.ReplaceAll(profit, ",", ""), 64); err == nil {
			sortValue = f
		}
	}
	resultsMap["grid"] = append(resultsMap["grid"], ResultItem{
		Email:      email,
		Result:     result,
		Timestamp:  time.Now().Format("15:04:05"),
		SortValue:  sortValue,
		CouponID:   couponID,
		StrategyID: strategyID,
		Profit:     profit,
		IP:         ip,
		Detail:     detail,
	})
}

// extractSortValue 根据不同的tab类型提取排序值
func extractSortValue(tab, result, usdt, badge string) float64 {
	switch tab {
	case "asset":
		// 资产查询: 结果格式 "总资产(USDT): 123.45"
		if strings.Contains(result, "总资产(USDT):") {
			val := strings.Replace(result, "总资产(USDT):", "", 1)
			val = strings.TrimSpace(val)
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	case "coupon":
		// 优惠券查询: badge存储优惠券数量
		if badge != "" {
			if f, err := strconv.ParseFloat(badge, 64); err == nil {
				return f
			}
		}
	case "return":
		// 红包雨: usdt存储USDT奖励
		if usdt != "" {
			val := strings.ReplaceAll(usdt, "USDT", "")
			val = strings.TrimSpace(val)
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	case "turntable":
		// 大转盘: badge存储抽奖结果金额
		if badge != "" {
			val := strings.ReplaceAll(badge, "USDT", "")
			val = strings.TrimSpace(val)
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	}
	return 0
}

func fetchProxyList(proxyURL string) ([]string, error) {
	if proxyURL == "" {
		return nil, fmt.Errorf("proxy URL is empty")
	}
	log.Printf("fetching proxy list from %s ...", proxyURL)
	resp, err := http.Get(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("fetch proxy failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read proxy response failed: %v", err)
	}

	var proxies []string
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
				line = "http://" + line
			}
			proxies = append(proxies, line)
		}
	}
	return proxies, nil
}

func loadAccounts(f string) []Account {
	var accs []Account
	file, err := os.Open(f)
	if err != nil {
		return accs
	}
	defer file.Close()

	s := bufio.NewScanner(file)
	for s.Scan() {
		l := strings.TrimSpace(s.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		parts := strings.SplitN(l, "----", 4)
		if len(parts) >= 2 {
			acc := Account{
				Email:    strings.TrimSpace(parts[0]),
				Password: strings.TrimSpace(parts[1]),
			}
			if len(parts) >= 3 {
				acc.UID = strings.TrimSpace(parts[2])
			}
			if len(parts) >= 4 {
				acc.GAKey = strings.TrimSpace(parts[3])
			}
			accs = append(accs, acc)
		}
	}
	return accs
}

func runLoginWeb(concurrency int, proxyFetchURL string, useProxy bool, proxyURL string) {
	if !tryStartTask("登录") {
		return
	}

	accounts := loadAccounts("accounts.txt")
	if len(accounts) == 0 {
		setStatus(false, "未加载到账号，请检查accounts.txt文件")
		return
	}

	setStatus(true, fmt.Sprintf("登录中... 共%d个账号，并发数: %d", len(accounts), concurrency))
	setProgress(0, len(accounts))

	loginResults = make([]LoginResult, len(accounts))
	for i := range loginResults {
		loginResults[i] = LoginResult{ID: i + 1, Email: accounts[i].Email, Status: "初始化中..."}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i, acc := range accounts {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(idx int, a Account) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			loginResults[idx].Status = "随机延迟中..."

			jitter := time.Duration(rand.Intn(600)+150) * time.Millisecond
			time.Sleep(jitter)

			var proxy string
			if useProxy && proxyURL != "" {
				proxy = proxyURL
			} else if proxyFetchURL != "" {
				if globalProxyPool == nil || globalProxyPool.proxyURL != proxyFetchURL {
					initProxyPool(proxyFetchURL)
				}
				proxy = globalProxyPool.fetchFromPool()
			}

			if proxy == "" {
				loginResults[idx].Status = "完成"
				loginResults[idx].Result = "代理获取失败"
				return
			}

			loginResults[idx].Status = "登录中..."

			success := false

			for attempt := 1; attempt <= 6; attempt++ {
				if attempt > 1 {
					loginResults[idx].Status = fmt.Sprintf("第%d次重试中...", attempt)
					time.Sleep(1000 * time.Millisecond)

					if !useProxy && proxyFetchURL != "" {
						if globalProxyPool == nil || globalProxyPool.proxyURL != proxyFetchURL {
							initProxyPool(proxyFetchURL)
						}
						newProxy := globalProxyPool.fetchFromPool()
						if newProxy != "" {
							proxy = newProxy
							log.Printf("[%d] 第%d次重试获取新代理: %s", idx+1, attempt, proxy)
						}
					}
				}

				profile := NewDeviceProfile()
				loginMgr := NewHTXLoginManager(profile, proxy)

				res := loginMgr.LoginFlow(a.Email, a.Password, a.GAKey)

				if res != nil {
					if s, ok := res["success"].(bool); ok && s {
						ucToken, _ := res["uc_token"].(string)
						vToken, _ := res["vtoken"].(string)

						newToken := SavedToken{
							Email:       a.Email,
							Password:    a.Password,
							GAKey:       a.GAKey,
							UCToken:     ucToken,
							VToken:      vToken,
							Fingerprint: loginMgr.Fingerprint,
							UA:          loginMgr.UA,
							UserAgent:   loginMgr.UserAgent,
							UID:         a.UID,
							LastLogin:   time.Now(),
						}

						if err := SaveOrUpdateToken(newToken); err != nil {
							log.Printf("[%d] 保存 Token 失败: %v", idx+1, err)
						}

						loginResults[idx].VToken = vToken
						loginResults[idx].UCToken = ucToken
						loginResults[idx].Status = "完成"
						loginResults[idx].Result = "登录成功"
						success = true
						break
					}
				}
			}

			if !success {
				loginResults[idx].Status = "完成"
				loginResults[idx].Result = "登录失败"
			}
		}(i, acc)
	}

	wg.Wait()
	setStatus(false, "登录执行完毕")
}

func runExchangeWeb(hour, minute, second, retryCount int, useProxy bool, advanceMs int, useServerTime bool) {
	if !tryStartTask("兑换勋章") {
		return
	}

	// 初始化兑换日志
	if err := initExchangeLog(); err != nil {
		log.Println("初始化日志失败:", err)
	} else {
		defer closeExchangeLog()
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token，请先运行登录获取Token")
		return
	}

	setStatus(true, fmt.Sprintf("兑换勋章中... 共%d个账号", len(tokens)))

	for email := range tokens {
		addResult("exchange", email, "预加载中...", "", "")
	}

	// ==================== 时间同步策略（与GUI端完全一致） ====================
	//   - 等待时钟用 NTP（真实时间，服务器 Date 头证实服务器对齐 NTP）
	//   - 目标刷新时刻用 HTX 的 timestamp + inventoryRefreshTime（服务器内部字段相加，稳定正确±4ms）
	//   - 不再用 HTX 粗糙的 timestamp 算 offset（它有秒级缓存误差，会引入 ~200ms 系统偏差）
	timeOffset := time.Duration(0)
	timeSource := "本机时间"
	var serverTargetTime time.Time     // 服务器报告的下次库存刷新绝对时刻
	var htxOffsetForDiag time.Duration // HTX timestamp 计算的偏移（仅诊断用）

	// 1. 从HTX服务器获取库存刷新时刻（目标用）+ HTX timestamp 偏移（诊断用）
	htxOffset, serverRefreshAt, err := syncHTXServerTime()
	if err == nil {
		htxOffsetForDiag = htxOffset
		serverTargetTime = serverRefreshAt
		log.Printf("[兑换勋章] ✓ 从HTX服务器获取到库存刷新时间: %s 北京时间",
			serverTargetTime.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05.000"))
	} else {
		log.Printf("[兑换勋章] HTX服务器刷新时间获取失败(%v)", err)
	}

	// 2. 等待时钟用 NTP（真实时间，服务器真实时钟对齐 NTP）
	ntpTime, errNTP := getNetworkBeijingTime()
	if errNTP == nil {
		timeOffset = time.Until(ntpTime)
		timeSource = "NTP时间"
		log.Printf("[兑换勋章] 等待时钟: %s | 偏移量=%v（本地时钟比NTP%v）",
			timeSource, timeOffset,
			func() string {
				if timeOffset > 0 {
					return fmt.Sprintf("慢%v", timeOffset)
				}
				return fmt.Sprintf("快%v", -timeOffset)
			}())
		// 诊断对比：NTP 与 HTX timestamp 偏移的差异
		if err == nil {
			log.Printf("[兑换勋章] 对时诊断: NTP偏移=%v | HTX timestamp偏移=%v | 两者偏差=%v（若偏差大说明HTX timestamp有缓存误差）",
				timeOffset, htxOffsetForDiag, timeOffset-htxOffsetForDiag)
		}
	} else {
		// NTP 失败，回退到 HTX timestamp offset（有缓存误差，精度较差）
		log.Printf("[兑换勋章] NTP时间获取失败(%v)，回退到HTX timestamp偏移", errNTP)
		if err == nil {
			timeOffset = htxOffset
			timeSource = "HTX服务器时间"
		} else {
			setStatus(false, "时间同步失败，使用本机时间")
		}
	}

	now := time.Now().Add(timeOffset)
	log.Printf("[兑换勋章] 时间源: %s | 校准后时间: %s | 本地时间: %s | 偏移量: %v",
		timeSource,
		now.Format("15:04:05.000"),
		time.Now().Format("15:04:05.000"),
		timeOffset)
	ExchangeTimeOffset = timeOffset

	// ==================== 小批量并发预加载 pro_token（带重试） ====================
	setStatus(true, "正在预加载 pro_token...")

	proTokens := make(map[string]string)
	proxyMap := make(map[string]string)
	var preloadWg sync.WaitGroup
	var preloadMu sync.Mutex

	maxConcurrent := 10
	maxRetry := 2

	sem := make(chan struct{}, maxConcurrent)

	for email, tk := range tokens {
		sem <- struct{}{}
		preloadWg.Add(1)

		go func(e string, t SavedToken) {
			defer preloadWg.Done()
			defer func() { <-sem }()

			proxy := ""
			if useProxy {
				proxy = ""
			}

			var proToken string
			var lastErr error

			for attempt := 0; attempt <= maxRetry; attempt++ {
				ticket, err1 := GetTicket(t.UCToken, t.Fingerprint, t.VToken, t.UA, t.UserAgent, proxy)
				if err1 != nil {
					lastErr = err1
					if attempt < maxRetry {
						time.Sleep(300 * time.Millisecond)
						continue
					}
					break
				}

				pToken, err2 := GetProToken(ticket, t.UserAgent, proxy)
				if err2 != nil {
					lastErr = err2
					if attempt < maxRetry {
						time.Sleep(300 * time.Millisecond)
						continue
					}
					break
				}

				proToken = pToken
				break
			}

			preloadMu.Lock()
			if proToken != "" {
				proTokens[e] = proToken
				proxyMap[e] = proxy
				updateResult("exchange", e, "pro_token已就绪")
			} else {
				updateResult("exchange", e, "预加载失败")
				if lastErr != nil {
					log.Printf("[预加载失败] %s: %v", e, lastErr)
				}
			}
			preloadMu.Unlock()
		}(email, tk)
	}

	preloadWg.Wait()

	WarmupConnections()
	setStatus(true, "pro_token 预加载完成，连接预热中...")
	time.Sleep(500 * time.Millisecond)

	// ==================== 计算目标时间 ====================
	now = time.Now().Add(timeOffset)
	if useServerTime {
		// 勾选了"用服务器刷新时间"，优先用服务器返回的库存刷新时刻
		if serverTargetTime.IsZero() {
			log.Printf("[兑换勋章] ❗ 服务器刷新时间获取失败，回退到自定义时间")
			useServerTime = false
		} else {
			serverTargetTime = serverTargetTime.In(now.Location())
			log.Printf("[兑换勋章] 使用服务器库存刷新时间作为目标: %s",
				serverTargetTime.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05.000"))
		}
	}

	var target time.Time
	if useServerTime {
		target = serverTargetTime
	} else {
		target = time.Date(now.Year(), now.Month(), now.Day(), hour, minute, second, 0, now.Location())
		if target.Before(now) {
			target = target.Add(24 * time.Hour)
		}
		log.Printf("[兑换勋章] 使用自定义时间作为目标: %s", target.Format("2006-01-02 15:04:05.000"))
	}

	// 如果距目标时间不足10秒（可能刚刷新完或时间已过），日志提示
	remain := target.Sub(now)
	if useServerTime && remain < 10*time.Second {
		if remain > 0 {
			log.Printf("[兑换勋章] 距刷新仅%v，立即发送！", remain)
		} else {
			log.Printf("[兑换勋章] 库存刷新时间已过%v，立即发送！", -remain)
		}
	}

	// 提前毫秒数（补偿网络RTT，让请求在目标时刻到达服务器）
	if advanceMs < 0 {
		advanceMs = 55
	}
	advanceDur := time.Duration(advanceMs) * time.Millisecond
	sendAt := target.Add(-advanceDur) // 实际发送时刻（网络时间）

	log.Printf("[兑换勋章] 目标时间: %s | 提前 %dms | 实际发送时刻: %s",
		target.Format("15:04:05.000"), advanceMs, sendAt.Format("15:04:05.000"))

	// ===== 阶段一：粗倒计时（100ms精度，直到剩余5秒） =====
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case <-ticker.C:
			current := time.Now().Add(timeOffset)
			remain := target.Sub(current)
			if remain <= 5*time.Second {
				goto preciseWait
			}
			setStatus(true, fmt.Sprintf("等待目标时间... 剩余 %02d:%02d:%02d",
				int(remain.Hours()), int(remain.Minutes())%60, int(remain.Seconds())%60))
		}
	}

preciseWait:
	// ===== 阶段二：主线程精确等待到sendAt，然后全量同时发送 =====
	maxSends := retryCount
	if maxSends <= 0 {
		maxSends = 40
	}

	var totalSent, successCount, failCount int64
	var wg sync.WaitGroup

	type accountStats struct {
		sent    int32
		success bool
	}
	stats := make(map[string]*accountStats)
	for email := range proTokens {
		stats[email] = &accountStats{}
	}

	log.Printf("[兑换勋章] 全量发送: %d 账号 × %d 次 = %d 请求", len(proTokens), maxSends, len(proTokens)*maxSends)

	setStatus(true, "精准对时中...")

	// 主线程精确等待到 sendAt
	for {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		default:
		}
		remain := sendAt.Sub(time.Now().Add(timeOffset))
		if remain <= 0 {
			break
		}
		switch {
		case remain > 2*time.Second:
			time.Sleep(500 * time.Millisecond)
		case remain > 200*time.Millisecond:
			time.Sleep(100 * time.Millisecond)
		case remain > 20*time.Millisecond:
			time.Sleep(10 * time.Millisecond)
		case remain > 2*time.Millisecond:
			time.Sleep(time.Millisecond)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	log.Printf("[兑换勋章] ✓ 到达发送时刻 %s，全量启动发送", sendAt.Format("15:04:05.000"))
	setStatus(true, "全量发送中...")

	for email, proToken := range proTokens {
		vToken := tokens[email].VToken
		for i := 0; i < maxSends; i++ {
			wg.Add(1)
			go func(e, pt, vt string) {
				defer wg.Done()

				atomic.AddInt64(&totalSent, 1)
				atomic.AddInt32(&stats[e].sent, 1)

				exchangeResult, err := ExchangeWelfareFast(pt, vt, e, proxyMap[e])

				if err == nil && exchangeResult.Success {
					atomic.AddInt64(&successCount, 1)
					if !stats[e].success {
						stats[e].success = true
						resultStr := fmt.Sprintf("成功(发送%d次)", stats[e].sent)
						if exchangeResult.AwardTitle != "" {
							resultStr += fmt.Sprintf(" - %s", exchangeResult.AwardTitle)
						}
						if exchangeResult.AwardID > 0 {
							resultStr += fmt.Sprintf("(ID:%d)", exchangeResult.AwardID)
						}
						if exchangeResult.RemainingFragments >= 0 {
							resultStr += fmt.Sprintf(",剩余碎片:%d", exchangeResult.RemainingFragments)
						}
						updateResult("exchange", e, resultStr)
					}
				} else {
					atomic.AddInt64(&failCount, 1)
					if !stats[e].success && stats[e].sent >= int32(maxSends) {
						resultStr := fmt.Sprintf("失败(发送%d次)", stats[e].sent)
						if exchangeResult != nil && exchangeResult.Message != "" {
							resultStr += fmt.Sprintf(" - %s", exchangeResult.Message)
						} else if err != nil {
							resultStr += fmt.Sprintf(" - %v", err)
						}
						updateResult("exchange", e, resultStr)
					}
				}
			}(email, proToken, vToken)
		}
	}

	wg.Wait()
	setStatus(false, fmt.Sprintf("兑换勋章执行完毕！总请求: %d, 成功: %d, 失败: %d",
		totalSent, successCount, failCount))
}

func updateResult(tab, email, result string) {
	for i := range resultsMap[tab] {
		if resultsMap[tab][i].Email == email {
			resultsMap[tab][i].Result = result
			return
		}
	}
}

func runSignInWeb(concurrency int, useProxy bool) {
	if !tryStartTask("签到") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token，请先运行登录获取Token")
		return
	}

	setStatus(true, fmt.Sprintf("签到中... 共%d个账号，并发数: %d", len(tokens), concurrency))
	setProgress(0, len(tokens))

	signInResults = make([]SignInResult, 0, len(tokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for email, tk := range tokens {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			result := doSignInWeb(email, tk, useProxy)
			signInResults = append(signInResults, result)
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "签到执行完毕")
}

func doSignInWeb(email string, tk SavedToken, useProxy bool) SignInResult {
	result := SignInResult{
		Email:         email,
		Status:        "初始化中...",
		OperationTime: time.Now().Format("15:04:05"),
	}

	proxy := ""
	if useProxy {
		proxy = ""
	}

	ticket, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, proxy)
	if err != nil {
		result.Status = "失败"
		result.WelfareSignStatus = fmt.Sprintf("获取Ticket失败: %v", err)
		return result
	}

	proToken, err := GetProToken(ticket, tk.UserAgent, proxy)
	if err != nil {
		result.Status = "失败"
		result.WelfareSignStatus = fmt.Sprintf("获取ProToken失败: %v", err)
		return result
	}

	badgeBefore, err := GetWelfareBadgeCountBeforeSignIn(tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.Fingerprint, proxy)
	if err != nil {
		result.Status = "失败"
		result.BadgeBefore = "-"
	} else {
		result.BadgeBefore = fmt.Sprintf("%d", badgeBefore)
	}

	welfareGold, err := GetWelfareGoldBalance(tk.UCToken, proToken, tk.VToken, tk.UserAgent, proxy)
	if err != nil {
		result.WelfareGold = "-"
	} else {
		result.WelfareGold = welfareGold
		if welfareGold != "" {
			saveHighWelfareAccount(email, welfareGold, tk)
		}
	}

	userTaskId, err := GetWelfareCheckInUserTaskId(tk.UCToken, proToken, tk.VToken, tk.UserAgent, proxy)
	if err != nil {
		result.Status = "失败"
		result.WelfareSignStatus = fmt.Sprintf("获取签到任务ID失败: %v", err)
		return result
	}

	welfareResult, err := DoWelfareUserSignIn(tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.UID, userTaskId, proxy)
	if err != nil {
		result.Status = "失败"
		result.WelfareSignStatus = fmt.Sprintf("福利签到失败: %v", err)
	} else {
		result.WelfareSignStatus = welfareResult
	}

	inviteTaskId, err := GetInviteCheckInUserTaskId(tk.UCToken, proToken, tk.VToken, tk.UserAgent, proxy)
	if err != nil {
		result.InviteSignStatus = "获取邀请签到任务ID失败"
	} else {
		inviteResult, err := DoInviteSignIn(tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.UID, inviteTaskId, proxy)
		if err != nil {
			result.InviteSignStatus = fmt.Sprintf("邀请签到失败: %v", err)
		} else {
			result.InviteSignStatus = inviteResult
		}
	}

	badgeAfter, err := GetWelfareBadgeCountBeforeSignIn(tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.Fingerprint, proxy)
	if err != nil {
		result.BadgeCount = "-"
	} else {
		result.BadgeCount = fmt.Sprintf("%d", badgeAfter)
	}

	if result.Status != "失败" {
		result.Status = "完成"
	}

	if badgeAfter > 100 {
		if err := SaveHighBadgeAccount(tk); err == nil {
			log.Printf("[签到] [%s] 勋章数量 %d > 100，已保存到高勋章文件", email, badgeAfter)
		}
	}

	return result
}

func runRetryFailedSignIn(concurrency int, useProxy bool) {
	if !tryStartTaskWithClear("重新运行失败账号", false) {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	var failedAccounts []string
	for _, r := range signInResults {
		isFailed := strings.Contains(r.Status, "失败") ||
			strings.Contains(r.WelfareSignStatus, "失败") ||
			strings.Contains(r.InviteSignStatus, "失败")
		if isFailed {
			failedAccounts = append(failedAccounts, r.Email)
		}
	}

	if len(failedAccounts) == 0 {
		setStatus(false, "当前没有失败的账号")
		return
	}

	setStatus(true, fmt.Sprintf("正在重新运行 %d 个失败账号...", len(failedAccounts)))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, email := range failedAccounts {
		tk, ok := tokens[email]
		if !ok {
			continue
		}

		sem <- struct{}{}
		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()

			result := doSignInWeb(email, tk, useProxy)
			for i := range signInResults {
				if signInResults[i].Email == email {
					signInResults[i] = result
					break
				}
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "失败账号重新运行完毕")
}

func runRetryLoginFailed(concurrency int) {
	if !tryStartTaskWithClear("重新登录失败账号", false) {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	var failedTokens []SavedToken
	for _, r := range signInResults {
		isFailed := strings.Contains(r.Status, "失败") ||
			strings.Contains(r.WelfareSignStatus, "失败") ||
			strings.Contains(r.InviteSignStatus, "失败")
		if isFailed {
			if tk, ok := tokens[r.Email]; ok {
				failedTokens = append(failedTokens, tk)
			}
		}
	}

	if len(failedTokens) == 0 {
		setStatus(false, "当前没有失败的账号")
		return
	}

	setStatus(true, fmt.Sprintf("正在重新登录 %d 个失败账号...", len(failedTokens)))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, tk := range failedTokens {
		sem <- struct{}{}
		wg.Add(1)

		go func(tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()

			profile := NewDeviceProfile()

			for attempt := 1; attempt <= 6; attempt++ {
				loginMgr := NewHTXLoginManager(profile, "")
				res := loginMgr.LoginFlow(tk.Email, tk.Password, tk.GAKey)

				if res != nil {
					if s, ok := res["success"].(bool); ok && s {
						ucToken, _ := res["uc_token"].(string)
						vToken, _ := res["vtoken"].(string)

						newToken := SavedToken{
							Email:       tk.Email,
							Password:    tk.Password,
							GAKey:       tk.GAKey,
							UCToken:     ucToken,
							VToken:      vToken,
							Fingerprint: loginMgr.Fingerprint,
							UA:          loginMgr.UA,
							UserAgent:   loginMgr.UserAgent,
							UID:         tk.UID,
							LastLogin:   time.Now(),
						}

						if err := SaveOrUpdateToken(newToken); err == nil {
							tokens[tk.Email] = newToken
						}
						break
					}
				}
				time.Sleep(1000 * time.Millisecond)
			}
		}(tk)
	}

	wg.Wait()
	setStatus(false, "失败账号重新登录完毕，可刷新Token列表后重新签到")
}

type TokenResult struct {
	email  string
	tk     SavedToken
	token1 string
	token2 string
	err    error
}

func runHongBaoWeb(concurrency int, useSchedule bool, hour, minute, second int) {
	if !tryStartTask("红包雨") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token，请先运行登录获取Token")
		return
	}

	setStatus(true, "正在执行红包雨...")
	setProgress(0, len(tokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for email, tk := range tokens {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			addResult("hongbao", email, "预获取Token...", "", "")

			res, err := processHongBaoRain(email, tk)
			if err != nil {
				addResult("hongbao", email, fmt.Sprintf("失败: %v", err), "", "")
				return
			}

			parts := strings.Split(res, "|||")
			if len(parts) < 2 {
				addResult("hongbao", email, "失败: token格式错误", "", "")
				return
			}

			addResult("hongbao", email, "领取中...", "", "")

			res, err = doHongBaoDraw(email, tk, parts[0], parts[1])
			if err != nil {
				addResult("hongbao", email, fmt.Sprintf("领取失败: %v", err), "", "")
			} else {
				var usdt string
				var badge string
				var awardIds []string
				var resultMap map[string]interface{}
				if json.Unmarshal([]byte(res), &resultMap) == nil {
					if data, ok := resultMap["data"].(map[string]interface{}); ok {
						if drawDetailList, ok := data["drawDetailList"].([]interface{}); ok {
							for _, item := range drawDetailList {
								if detail, ok := item.(map[string]interface{}); ok {
									if awardId, ok := detail["awardId"].(float64); ok {
										title := fmt.Sprintf("奖品%d", int(awardId))
										if t, ok := detail["title"].(string); ok && t != "" {
											title = t
										}
										awardIds = append(awardIds, fmt.Sprintf("%d-%s", int(awardId), title))
									}
									if props, ok := detail["properties"].(map[string]interface{}); ok {
										if currency, ok := props["currency"].(string); ok && currency == "usdt" {
											if count, ok := props["count"].(float64); ok {
												usdt = fmt.Sprintf("%.2f", count)
											}
										}
										if name, ok := props["name"].(string); ok && name != "" {
											badge = name
										}
									}
								}
							}
						}
					}
				}
				addResult("hongbao", email, "领取成功: "+strings.Join(awardIds, ","), usdt, badge)
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "红包雨执行完毕")
}

func runReturnRewardWeb(concurrency int) {
	if !tryStartTask("领取回归奖励") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token，请先运行登录获取Token")
		return
	}

	setStatus(true, fmt.Sprintf("领取回归奖励中... 共%d个账号，并发数: %d", len(tokens), concurrency))
	setProgress(0, len(tokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for email, tk := range tokens {
		sem <- struct{}{}
		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			result, usdt, badge := doReturnRewardWeb(email, tk)
			addResult("return", email, result, usdt, badge)
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "领取回归奖励执行完毕")
}

func runTurntableWeb(req TurntableRequest) {
	if !tryStartTask("大转盘抽奖") {
		return
	}

	if req.ActivityId == "" {
		setStatus(false, "请填写抽奖活动ID")
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token，请先运行登录获取Token")
		return
	}

	if req.Concurrency <= 0 {
		req.Concurrency = 8
	}

	if req.UseSchedule {
		delaySeconds := int64(req.Hour)*3600 + int64(req.Minute)*60 + int64(req.Second)
		if delaySeconds > 0 {
			setStatus(true, fmt.Sprintf("正在倒计时 %d 秒...", delaySeconds))
			for i := delaySeconds; i > 0; i-- {
				select {
				case <-stopChan:
					setStatus(false, "已停止")
					return
				default:
				}
				setStatus(true, fmt.Sprintf("正在倒计时 %d 秒...", i))
				time.Sleep(1 * time.Second)
			}
		}
	}

	setStatus(true, fmt.Sprintf("正在执行大转盘抽奖... 共%d个账号，并发数: %d", len(tokens), req.Concurrency))
	setProgress(0, len(tokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, req.Concurrency)

	for email, tk := range tokens {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			result, award, ip, err := ProcessTurntable(email, tk, req.ActivityId, func() string { return "" })
			if err != nil {
				addResult("turntable", email, fmt.Sprintf("失败: %v", err), ip, award)
			} else {
				addResult("turntable", email, result, ip, award)
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "大转盘抽奖执行完毕")
}

func runTurntableRetryFailed(activityId string, concurrency int) {
	if !tryStartTask("大转盘重新运行失败账号") {
		return
	}

	if activityId == "" {
		setStatus(false, "请填写抽奖活动ID")
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	var failedEmails []string
	for _, item := range resultsMap["turntable"] {
		if strings.Contains(item.Result, "失败") {
			failedEmails = append(failedEmails, item.Email)
		}
	}

	if len(failedEmails) == 0 {
		setStatus(false, "没有失败的账号")
		return
	}

	setStatus(true, fmt.Sprintf("正在重新运行 %d 个失败账号...", len(failedEmails)))
	setProgress(0, len(failedEmails))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, email := range failedEmails {
		tk, ok := tokens[email]
		if !ok {
			continue
		}

		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			result, award, ip, err := ProcessTurntable(email, tk, activityId, func() string { return "" })
			if err != nil {
				addResult("turntable", email, fmt.Sprintf("失败: %v", err), ip, award)
			} else {
				addResult("turntable", email, result, ip, award)
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "大转盘失败账号重新运行完毕")
}

func runTurntableRetryLogin(concurrency int) {
	if !tryStartTask("大转盘重新登录失败账号") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	var failedTokens []SavedToken
	for _, item := range resultsMap["turntable"] {
		if strings.Contains(item.Result, "失败") {
			if tk, ok := tokens[item.Email]; ok {
				failedTokens = append(failedTokens, tk)
			}
		}
	}

	if len(failedTokens) == 0 {
		setStatus(false, "没有失败的账号")
		return
	}

	setStatus(true, fmt.Sprintf("正在重新登录 %d 个失败账号...", len(failedTokens)))
	setProgress(0, len(failedTokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, tk := range failedTokens {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			profile := NewDeviceProfile()
			var success bool

			for attempt := 1; attempt <= 6; attempt++ {
				if attempt > 1 {
					time.Sleep(1000 * time.Millisecond)
				}

				loginMgr := NewHTXLoginManager(profile, "")
				res := loginMgr.LoginFlow(tk.Email, tk.Password, tk.GAKey)

				if res != nil {
					if s, ok := res["success"].(bool); ok && s {
						ucToken, _ := res["uc_token"].(string)
						vToken, _ := res["vtoken"].(string)

						newToken := SavedToken{
							Email:       tk.Email,
							Password:    tk.Password,
							GAKey:       tk.GAKey,
							UCToken:     ucToken,
							VToken:      vToken,
							Fingerprint: loginMgr.Fingerprint,
							UA:          loginMgr.UA,
							UserAgent:   loginMgr.UserAgent,
							UID:         tk.UID,
							LastLogin:   time.Now(),
						}

						if err := SaveOrUpdateToken(newToken); err != nil {
							log.Printf("[大转盘重登] 账号 %s 保存 Token 失败: %v", tk.Email, err)
						} else {
							log.Printf("[大转盘重登] 账号 %s 登录成功，Token已更新", tk.Email)
						}
						success = true
						break
					}
				}
			}

			if success {
				log.Printf("[大转盘重登] 账号 %s 重新登录成功", tk.Email)
			} else {
				log.Printf("[大转盘重登] 账号 %s 重新登录失败", tk.Email)
			}
		}(tk)
	}

	wg.Wait()
	setStatus(false, "大转盘失败账号重新登录完毕，可刷新Token列表后重新抽奖")
}

func runCouponQuery(concurrency int, useProxy bool) {
	if !tryStartTask("查询优惠券") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	setStatus(true, fmt.Sprintf("开始查询 %d 个账号的优惠券...", len(tokens)))
	setProgress(0, len(tokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for email, tk := range tokens {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			result, count, ip, err := ProcessQueryCoupon(email, tk, func() string { return "" }, useProxy)
			if err != nil {
				addResult("coupon", email, fmt.Sprintf("失败: %v", err), ip, "")
			} else {
				addResult("coupon", email, result, ip, count)
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "优惠券查询执行完毕")
}

func runCouponRetryFailed(concurrency int, useProxy bool) {
	if !tryStartTask("优惠券重新运行失败账号") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	var failedEmails []string
	for _, item := range resultsMap["coupon"] {
		if strings.Contains(item.Result, "失败") {
			failedEmails = append(failedEmails, item.Email)
		}
	}

	if len(failedEmails) == 0 {
		setStatus(false, "没有失败的账号")
		return
	}

	setStatus(true, fmt.Sprintf("正在重新运行 %d 个失败账号...", len(failedEmails)))
	setProgress(0, len(failedEmails))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, email := range failedEmails {
		tk, ok := tokens[email]
		if !ok {
			continue
		}

		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			result, count, ip, err := ProcessQueryCoupon(email, tk, func() string { return "" }, useProxy)
			if err != nil {
				addResult("coupon", email, fmt.Sprintf("失败: %v", err), ip, "")
			} else {
				addResult("coupon", email, result, ip, count)
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "优惠券失败账号重新运行完毕")
}

func runCouponRetryLogin(concurrency int) {
	if !tryStartTask("优惠券重新登录失败账号") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	var failedTokens []SavedToken
	for _, item := range resultsMap["coupon"] {
		if strings.Contains(item.Result, "失败") {
			if tk, ok := tokens[item.Email]; ok {
				failedTokens = append(failedTokens, tk)
			}
		}
	}

	if len(failedTokens) == 0 {
		setStatus(false, "没有失败的账号")
		return
	}

	setStatus(true, fmt.Sprintf("正在重新登录 %d 个失败账号...", len(failedTokens)))
	setProgress(0, len(failedTokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, tk := range failedTokens {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			profile := NewDeviceProfile()
			var success bool

			for attempt := 1; attempt <= 6; attempt++ {
				if attempt > 1 {
					time.Sleep(1000 * time.Millisecond)
				}

				loginMgr := NewHTXLoginManager(profile, "")
				res := loginMgr.LoginFlow(tk.Email, tk.Password, tk.GAKey)

				if res != nil {
					if s, ok := res["success"].(bool); ok && s {
						ucToken, _ := res["uc_token"].(string)
						vToken, _ := res["vtoken"].(string)

						newToken := SavedToken{
							Email:       tk.Email,
							Password:    tk.Password,
							GAKey:       tk.GAKey,
							UCToken:     ucToken,
							VToken:      vToken,
							Fingerprint: loginMgr.Fingerprint,
							UA:          loginMgr.UA,
							UserAgent:   loginMgr.UserAgent,
							UID:         tk.UID,
							LastLogin:   time.Now(),
						}

						if err := SaveOrUpdateToken(newToken); err != nil {
							log.Printf("[优惠券重登] 账号 %s 保存 Token 失败: %v", tk.Email, err)
						} else {
							log.Printf("[优惠券重登] 账号 %s 登录成功，Token已更新", tk.Email)
						}
						success = true
						break
					}
				}
			}

			if success {
				log.Printf("[优惠券重登] 账号 %s 重新登录成功", tk.Email)
			} else {
				log.Printf("[优惠券重登] 账号 %s 重新登录失败", tk.Email)
			}
		}(tk)
	}

	wg.Wait()
	setStatus(false, "优惠券失败账号重新登录完毕")
}

func runGridOrder(concurrency int, useProxy bool, delaySeconds int, strategyType int) {
	if !tryStartTask("现货网格下单") {
		return
	}

	tokens, order, err := LoadTokensOrdered()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	strategyName := "现货网格"
	if strategyType == 1 {
		strategyName = "马丁格尔"
	}
	setStatus(true, fmt.Sprintf("开始为 %d 个账号下单%s...", len(tokens), strategyName))
	setProgress(0, len(tokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	// 使用原始顺序
	emails := order
	if len(emails) == 0 {
		emails = make([]string, 0, len(tokens))
		for email := range tokens {
			emails = append(emails, email)
		}
	}

	for _, email := range emails {
		tk := tokens[email]

		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		if delaySeconds > 0 {
			select {
			case <-stopChan:
				setStatus(false, "已停止")
				return
			case <-time.After(time.Duration(delaySeconds) * time.Second):
			}
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			couponID, strategyID, profit, detail, ip, err := ProcessGridOrder(email, tk, func() string { return "" }, useProxy, strategyType)
			if err != nil {
				addGridResult(email, "失败", "", "", "", ip, err.Error())
			} else {
				addGridResult(email, "完成", couponID, strategyID, profit, ip, detail)
				saveGridOrderResult(email, tk)
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, fmt.Sprintf("%s下单执行完毕", strategyName))
}

func runGridOrderRetryFailed(concurrency int, useProxy bool, delaySeconds int, strategyType int) {
	if !tryStartTask("现货网格重新运行失败账号") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	var failedEmails []string
	for _, item := range resultsMap["grid"] {
		if strings.Contains(item.Result, "失败") {
			failedEmails = append(failedEmails, item.Email)
		}
	}

	if len(failedEmails) == 0 {
		setStatus(false, "没有失败的账号")
		return
	}

	setStatus(true, fmt.Sprintf("正在重新运行 %d 个失败账号...", len(failedEmails)))
	setProgress(0, len(failedEmails))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, email := range failedEmails {
		tk, ok := tokens[email]
		if !ok {
			continue
		}

		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		if delaySeconds > 0 {
			select {
			case <-stopChan:
				setStatus(false, "已停止")
				return
			case <-time.After(time.Duration(delaySeconds) * time.Second):
			}
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			couponID, strategyID, profit, detail, ip, err := ProcessGridOrder(email, tk, func() string { return "" }, useProxy, strategyType)
			if err != nil {
				addGridResult(email, "失败", "", "", "", ip, err.Error())
			} else {
				addGridResult(email, "完成", couponID, strategyID, profit, ip, detail)
				saveGridOrderResult(email, tk)
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "失败账号重新运行完毕")
}

func runGridOrderRetryLogin(concurrency int) {
	if !tryStartTask("现货网格重新登录失败账号") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	var failedTokens []SavedToken
	for _, item := range resultsMap["grid"] {
		if strings.Contains(item.Result, "失败") {
			if tk, ok := tokens[item.Email]; ok {
				failedTokens = append(failedTokens, tk)
			}
		}
	}

	if len(failedTokens) == 0 {
		setStatus(false, "没有失败的账号")
		return
	}

	setStatus(true, fmt.Sprintf("正在重新登录 %d 个失败账号...", len(failedTokens)))
	setProgress(0, len(failedTokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, tk := range failedTokens {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		wg.Add(1)

		go func(tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			profile := NewDeviceProfile()
			var success bool

			for attempt := 1; attempt <= 6; attempt++ {
				if attempt > 1 {
					time.Sleep(1000 * time.Millisecond)
				}

				loginMgr := NewHTXLoginManager(profile, "")
				res := loginMgr.LoginFlow(tk.Email, tk.Password, tk.GAKey)

				if res != nil {
					if s, ok := res["success"].(bool); ok && s {
						ucToken, _ := res["uc_token"].(string)
						vToken, _ := res["vtoken"].(string)

						newToken := SavedToken{
							Email:       tk.Email,
							Password:    tk.Password,
							GAKey:       tk.GAKey,
							UCToken:     ucToken,
							VToken:      vToken,
							Fingerprint: loginMgr.Fingerprint,
							UA:          loginMgr.UA,
							UserAgent:   loginMgr.UserAgent,
							UID:         tk.UID,
							LastLogin:   time.Now(),
						}

						if err := SaveOrUpdateToken(newToken); err != nil {
							log.Printf("[现货网格重登] 账号 %s 保存 Token 失败: %v", tk.Email, err)
						} else {
							log.Printf("[现货网格重登] 账号 %s 登录成功，Token已更新", tk.Email)
						}
						success = true
						break
					}
				}
			}

			if success {
				log.Printf("[现货网格重登] 账号 %s 重新登录成功", tk.Email)
			} else {
				log.Printf("[现货网格重登] 账号 %s 重新登录失败", tk.Email)
			}
		}(tk)
	}

	wg.Wait()
	setStatus(false, "现货网格失败账号重新登录完毕")
}

func runGridProfitQuery(concurrency int, useProxy bool, delaySeconds int, strategyType int) {
	if !tryStartTask("现货网格查询收益") {
		return
	}

	tokens, order, err := LoadTokensOrdered()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	strategyName := "现货网格"
	if strategyType == 1 {
		strategyName = "马丁格尔"
	}
	setStatus(true, fmt.Sprintf("开始查询 %d 个账号的%s收益...", len(tokens), strategyName))
	setProgress(0, len(tokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	// 使用原始顺序
	emails := order
	if len(emails) == 0 {
		emails = make([]string, 0, len(tokens))
		for email := range tokens {
			emails = append(emails, email)
		}
	}

	for _, email := range emails {
		tk := tokens[email]

		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		if delaySeconds > 0 {
			select {
			case <-stopChan:
				setStatus(false, "已停止")
				return
			case <-time.After(time.Duration(delaySeconds) * time.Second):
			}
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			profit, ip, err := ProcessGridProfitQuery(email, tk, func() string { return "" }, useProxy, strategyType)
			if err != nil {
				addResult("grid", email, fmt.Sprintf("查询失败: %v", err), ip, "")
			} else {
				addResult("grid", email, fmt.Sprintf("收益: %s", profit), ip, "")
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, fmt.Sprintf("%s收益查询完毕", strategyName))
}

func runAssetQuery(concurrency int, useProxy bool, delaySeconds int) {
	if !tryStartTask("查询资产") {
		return
	}

	tokens, err := LoadTokens()
	if err != nil {
		setStatus(false, fmt.Sprintf("加载Token失败: %v", err))
		return
	}

	if len(tokens) == 0 {
		setStatus(false, "没有找到任何Token")
		return
	}

	setStatus(true, fmt.Sprintf("开始查询 %d 个账号的资产...", len(tokens)))
	setProgress(0, len(tokens))

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for email, tk := range tokens {
		select {
		case <-stopChan:
			setStatus(false, "已停止")
			return
		case sem <- struct{}{}:
		}

		if delaySeconds > 0 {
			select {
			case <-stopChan:
				setStatus(false, "已停止")
				return
			case <-time.After(time.Duration(delaySeconds) * time.Second):
			}
		}

		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()
			defer incrementProgress()

			balance, ip, err := ProcessAssetQuery(email, tk, func() string { return "" }, useProxy)
			if err != nil {
				addResult("asset", email, fmt.Sprintf("查询失败: %v", err), ip, "")
			} else {
				addResult("asset", email, fmt.Sprintf("总资产(USDT): %s", balance), ip, "")
				saveAssetResult(email, balance)
			}
		}(email, tk)
	}

	wg.Wait()
	setStatus(false, "资产查询完毕")
}

func doReturnRewardWeb(email string, tk SavedToken) (string, string, string) {
	ticket, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, "")
	if err != nil {
		return fmt.Sprintf("获取Ticket失败: %v", err), "", ""
	}

	proToken, err := GetProToken(ticket, tk.UserAgent, "")
	if err != nil {
		return fmt.Sprintf("获取ProToken失败: %v", err), "", ""
	}

	tasks, err := GetAllUnreceivedAwards(tk.UCToken, proToken, tk.VToken, tk.UserAgent, "")
	if err != nil {
		return fmt.Sprintf("查询未领取奖励失败: %v", err), "", ""
	}

	if len(tasks) == 0 {
		return "无未领取奖励", "", ""
	}

	awards, err := DrawMultipleTaskPrize(tk.UCToken, proToken, tk.VToken, tk.UserAgent, tasks, "")
	if err != nil {
		return fmt.Sprintf("领取失败: %v", err), "", ""
	}

	var usdtList []string
	var badgeList []string
	for _, a := range awards {
		if a.Type == 1 {
			usdtList = append(usdtList, fmt.Sprintf("%g%s", a.Count, a.Currency))
		} else if a.Type == 6 {
			badgeName := a.Name
			if badgeName == "" {
				badgeName = "徽章"
			}
			badgeList = append(badgeList, fmt.Sprintf("%g个%s", a.Count, badgeName))
		}
	}

	usdtReward := ""
	if len(usdtList) > 0 {
		usdtReward = joinStrings(usdtList, ",")
	}

	badgeReward := ""
	if len(badgeList) > 0 {
		badgeReward = joinStrings(badgeList, ",")
	}

	return fmt.Sprintf("成功领取%d个奖励", len(awards)), usdtReward, badgeReward
}

func tryStartTask(taskName string) bool {
	return tryStartTaskWithClear(taskName, true)
}

func tryStartTaskWithClear(taskName string, clearResults bool) bool {
	statusMu.Lock()
	defer statusMu.Unlock()
	if taskStatus.Running {
		return false
	}
	taskStatus.Running = true
	taskStatus.Message = fmt.Sprintf("%s中...", taskName)
	stopChan = make(chan struct{})
	if clearResults {
		loginResults = []LoginResult{}
		signInResults = []SignInResult{}
		resultsMap["exchange"] = []ResultItem{}
		resultsMap["hongbao"] = []ResultItem{}
		resultsMap["return"] = []ResultItem{}
	}
	return true
}

func getNetworkBeijingTime() (time.Time, error) {
	ntpTime, err := ntp.Time("time.google.com")
	if err != nil {
		ntpTime, err = ntp.Time("ntp.aliyun.com")
	}
	if err != nil {
		return time.Time{}, err
	}
	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	return ntpTime.In(beijing), nil
}

// syncHTXServerTime 直接从HTX福利系统接口获取服务器时间和库存刷新倒计时
// 多次采样取RTT最小的样本，带NTP网络延迟补偿
// 返回:
//   - offset = HTX服务器时间 - 本地时间（加到 time.Now() 上即为HTX服务器时间）
//   - nextRefreshAt = 下次库存刷新的服务器绝对时刻 = timestamp + inventoryRefreshTime
//     注意：这个时刻直接由服务器的两个字段相加得到，不依赖本地时钟和offset，
//     是最准的刷新时刻（测试20次采样跨度仅4ms）
func syncHTXServerTime() (offset time.Duration, nextRefreshAt time.Time, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	apiURL := "https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/fragment/exchangeZone"

	type sample struct {
		offset           time.Duration
		rtt              time.Duration
		inventoryRefresh int64
		serverTimestamp  int64
	}

	var best *sample
	const samples = 5
	var lastErr string

	for i := 0; i < samples; i++ {
		req, reqErr := http.NewRequest("GET", apiURL, nil)
		if reqErr != nil {
			lastErr = reqErr.Error()
			continue
		}
		req.Header.Set("accept-language", "zh-CN")
		req.Header.Set("content-type", "application/json;charset=UTF-8")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
		req.Header.Set("origin", "https://www.htx.net.im")
		req.Header.Set("referer", "https://www.htx.net.im/zh-cn/welfare/?taskType=BadgeMall")

		t1 := time.Now()
		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr.Error()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t4 := time.Now()

		var result struct {
			Code int `json:"code"`
			Data []struct {
				Timestamp            int64 `json:"timestamp"`
				InventoryRefreshTime int64 `json:"inventoryRefreshTime"`
			} `json:"data"`
		}
		if jsonErr := json.Unmarshal(body, &result); jsonErr != nil {
			lastErr = fmt.Sprintf("JSON解析失败: %v, body前100字: %s", jsonErr, string(body[:min(100, len(body))]))
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if result.Code != 200 || len(result.Data) == 0 || result.Data[0].Timestamp == 0 {
			lastErr = fmt.Sprintf("响应异常 code=%d dataLen=%d", result.Code, len(result.Data))
			time.Sleep(200 * time.Millisecond)
			continue
		}

		serverTime := time.UnixMilli(result.Data[0].Timestamp)
		rtt := t4.Sub(t1)
		localMid := t1.Add(rtt / 2)
		off := serverTime.Sub(localMid)

		s := &sample{
			offset:           off,
			rtt:              rtt,
			inventoryRefresh: result.Data[0].InventoryRefreshTime,
			serverTimestamp:  result.Data[0].Timestamp,
		}
		if best == nil || rtt < best.rtt {
			best = s
		}

		if i < samples-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	if best == nil {
		return 0, time.Time{}, fmt.Errorf("5次采样全部失败，最后错误: %s", lastErr)
	}

	// 计算下次刷新时刻
	nextRefreshTime := time.UnixMilli(best.serverTimestamp + best.inventoryRefresh)
	log.Printf("[HTX时间] RTT=%v | 偏移量=%v（本地时钟比HTX服务器%v）",
		best.rtt, best.offset,
		func() string {
			if best.offset > 0 {
				return fmt.Sprintf("慢%v", best.offset)
			}
			return fmt.Sprintf("快%v", -best.offset)
		}())
	log.Printf("[HTX库存] 距下次刷新: %v | 下次刷新时刻(服务器): %s",
		time.Duration(best.inventoryRefresh)*time.Millisecond,
		nextRefreshTime.UTC().Format("2006-01-02 15:04:05.000 UTC")+" = "+
			nextRefreshTime.In(time.FixedZone("CST", 8*3600)).Format("15:04:05.000")+" 北京时间")

	return best.offset, nextRefreshTime, nil
}

func splitTokens(tokenStr string) []string {
	var result []string
	current := ""
	for i := 0; i < len(tokenStr); i++ {
		if i+2 < len(tokenStr) && tokenStr[i] == '|' && tokenStr[i+1] == '|' && tokenStr[i+2] == '|' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
			i += 2
		} else {
			current += string(tokenStr[i])
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

func saveHighWelfareAccount(email string, welfareGold string, tk SavedToken) {
	dir := "./high_welfare"
	os.MkdirAll(dir, 0755)

	data := fmt.Sprintf("%s----%s----%s----%s----%s----%s----%s----%s----%s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		email,
		welfareGold,
		tk.UCToken,
		tk.VToken,
		tk.Fingerprint,
		tk.UserAgent,
		tk.UID,
		tk.Password,
	)

	filePath := fmt.Sprintf("%s/%s_%s.txt", dir, email, welfareGold)
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[Save] 保存高福利金账号失败: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(data); err != nil {
		log.Printf("[Save] 写入文件失败: %v", err)
	}
}
