package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Part_NetworkInfo struct {
	Version     int
	Subversion  string
	Connections int
}

type Part_Blockchaininfo struct {
	Blocks int
}

type Part_Stakinginfo struct {
	Staking bool
	Cause   string
}

type ParticldStatus struct {
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Peers     string `json:"peers"`
	LastBlock string `json:"last_block"`
	Version   string `json:"version"`
	Staking   string `json:"staking"`
}

type RpcResponse struct {
	Result interface{}
	Err    string `json:"error"`
	Id     int
}

type TGQueryResult struct {
	Ok          bool        `json:"ok"`
	Error_code  int         `json:"error_code"`
	Description string      `json:"description"`
	Result      interface{} `json:"result"`
}

type TGGetUpdate struct {
	Offset  int `json:"offset"`
	Timeout int `json:"timeout"`
}

type TGGetChat struct {
	Chat_id string `json:"chat_id"`
}

type TGSendMessage struct {
	Chat_id                  int64  `json:"chat_id"`
	Text                     string `json:"text"`
	Parse_mode               string `json:"parse_mode"`
	Disable_web_page_preview bool   `json:"disable_web_page_preview"`
	Disable_notification     bool   `json:"disable_notification"`
}

type TGUser struct {
	Id            int    `json:"id"`
	Is_bot        bool   `json:"is_bot"`
	First_name    string `json:"first_name"`
	Last_name     string `json:"last_name"`
	Username      string `json:"username"`
	Language_code string `json:"language_code"`
}

type TGChat struct {
	Id         int64  `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Username   string `json:"username"`
	First_name string `json:"first_name"`
	Last_name  string `json:"last_name"`
}

type TGMessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	Url    string `json:"url"`
	User   TGUser `json:"user"`
}

type TGMessage struct {
	Message_id       int               `json:"message_id"`
	From             TGUser            `json:"from"`
	Date             int               `json:"date"`
	Chat             TGChat            `json:"chat"`
	Text             string            `json:"text"`
	Entities         []TGMessageEntity `json:"entities"`
	New_chat_members []TGUser          `json:"new_chat_members"`
	Left_chat_member TGUser            `json:"left_chat_member"`
}

type TGUpdate struct {
	Update_id      int       `json:"update_id"`
	Message        TGMessage `json:"message"`
	Edited_message TGMessage `json:"edited_message"`
}

type Config struct {
	Port                  int
	ParticldRpcPort       int
	ParticldDataDir       string
	ParticldStakingWallet string
	DbUrl                 string
}

type TGConfig struct {
	BotName           string
	BotAuth           string
	StatusMsgHour     int
	StatusMsgMinute   int
	StatusMsgChatName string
}

var g_prgName = "stakepoolInfoServer"
var g_particldAuth = ""
var g_particldStatus ParticldStatus
var g_particldStatusMutex sync.Mutex
var g_config = Config{9100, 51735, "", "", ""}
var g_httpServer *http.Server
var g_tgConfig TGConfig

func readConfig(filename string) bool {
	data, err := ioutil.ReadFile(filename)

	if err != nil {
		fmt.Printf("Failed to open config file \"%s\": %s\n", filename, err.Error())
		return false
	}

	err = json.Unmarshal(data, &g_config)
	if err != nil {
		fmt.Printf("Syntax error in config file %s: %v", filename, err)
		return false
	}

	return true
}

func readTelegramConfig(filename string) bool {
	data, err := ioutil.ReadFile(filename)

	if err != nil {
		fmt.Printf("Failed to open Telegram config file \"%s\": %s\n", filename, err.Error())
		return false
	}

	err = json.Unmarshal(data, &g_tgConfig)
	if err != nil {
		fmt.Printf("Syntax error in Telegram config file %s: %v", filename, err)
		return false
	}

	return true
}

func readParticldCookie() bool {
	path := fmt.Sprintf("%s/.cookie", g_config.ParticldDataDir)
	data, err := ioutil.ReadFile(path)

	if err != nil {
		fmt.Printf("Failed to read particld cookie file \"%s\": %s\n", path, err.Error())
		return false
	}

	g_particldAuth = strings.TrimSpace(string(data))

	return true
}

func execRpcJson(res interface{}, addr string, cmd string) bool {
	data, err := json.Marshal(map[string]interface{}{
		"method": cmd,
		"id":     2,
		"params": []interface{}{},
	})
	if err != nil {
		fmt.Printf("RPC: Marshal: %v", err)
		return false
	}
	resp, err := http.Post(addr, "application/json", strings.NewReader(string(data)))
	if err != nil {
		fmt.Printf("RPC: Post: %v", err)
		return false
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("RPC: ReadAll: %v", err)
		return false
	}
	//result := make(map[string]interface{})
	//fmt.Println(string(body))
	//fmt.Println(resp.Status)

	if resp.StatusCode != 200 {
		fmt.Printf("RPC: Bad response status: %s", resp.Status)
		return false
	}

	response := RpcResponse{}
	response.Result = res

	err = json.Unmarshal(body, &response)
	if err != nil {
		fmt.Printf("RPC: Unmarshal: %v", err)
		return false
	}

	//fmt.Printf("Result: Id: %d, error: %s\n", response.Id, response.Err)

	return true
}

func particldStatusCollector() {
	statusError := "communication error"
	na := "n/a"

	for {
		status := ParticldStatus{"", na, na, na, na, na}

		if readParticldCookie() {
			url := fmt.Sprintf("http://%s@localhost:%d/", g_particldAuth, g_config.ParticldRpcPort)

			var nwinfo Part_NetworkInfo
			if execRpcJson(&nwinfo, url, "getnetworkinfo") {
				status.Version = nwinfo.Subversion
				status.Peers = fmt.Sprintf("%d", nwinfo.Connections)
			} else {
				status.Status = statusError
			}

			var bcinfo Part_Blockchaininfo
			if execRpcJson(&bcinfo, url, "getblockchaininfo") {
				status.LastBlock = fmt.Sprintf("%d", bcinfo.Blocks)
			} else {
				status.Status = statusError
			}

			var stakeinfo Part_Stakinginfo
			var urlWallet string
			if g_config.ParticldStakingWallet != "" {
				urlWallet = fmt.Sprintf("%swallet/%s", url, g_config.ParticldStakingWallet)
			} else {
				urlWallet = url
			}
			if !execRpcJson(&stakeinfo, urlWallet, "getstakinginfo") {
				status.Status = statusError
			}

			var uptime int64

			if execRpcJson(&uptime, url, "uptime") {
				status.Uptime = fmt.Sprintf("%.1f days", float64(uptime)/3600/24)
			} else {
				status.Status = statusError
			}

			if status.Status != statusError {
				if stakeinfo.Staking {
					status.Status = "OK"
				} else {
					status.Status = fmt.Sprintf("not staking: %s", stakeinfo.Cause)
				}
			}
		} else {
			status.Status = statusError
		}

		g_particldStatusMutex.Lock()
		g_particldStatus = status
		g_particldStatusMutex.Unlock()

		time.Sleep(60 * time.Second)
	}
}

func handleDaemonStats(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	var status ParticldStatus

	g_particldStatusMutex.Lock()
	status = g_particldStatus
	g_particldStatusMutex.Unlock()

	data, err := json.Marshal(status)
	if err != nil {
		fmt.Printf("Marshal: %v", err)
	}
	io.WriteString(resp, string(data))

}

func telegramCall(in, out interface{}, request string, timeout time.Duration) bool {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", g_tgConfig.BotAuth, request)
	//fmt.Printf("telegramCall: URL: %s\n", url)

	client := &http.Client{
		Timeout: timeout,
	}

	data, err := json.Marshal(in)
	if err != nil {
		fmt.Printf("telegramCall: Marshal: %v", err)
		return false
	}

	resp, err := client.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		fmt.Printf("telegramCall: Post: %v", err)
		return false
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("telegramCall: ReadAll: %v", err)
		return false
	}
	//result := make(map[string]interface{})
	//fmt.Println(string(body))
	//fmt.Println(resp.Status)

	if resp.StatusCode != 200 {
		fmt.Printf("telegramCall: Bad response status: %s", resp.Status)
		return false
	}

	var result TGQueryResult

	result.Result = out

	err = json.Unmarshal(body, &result)
	if err != nil {
		fmt.Printf("telegramCall: Unmarshal: %v", err)
		return false
	}

	if !result.Ok {
		fmt.Printf("telegramCall: query failed: %s\n", result.Description)
		return false
	}

	return true
}

func telegramSendMessage(chatId int64, msg string) bool {
	var req TGSendMessage

	req.Chat_id = chatId
	req.Text = msg
	req.Parse_mode = "Markdown"
	req.Disable_notification = false
	req.Disable_web_page_preview = true

	var res TGMessage
	if !telegramCall(req, &res, "sendMessage", 10*time.Second) {
		n := 20
		if len(msg) < n {
			n = len(msg)
		}
		fmt.Printf("telegramSendMessage: failed: chat_id: %d, msg: \"%s\"", chatId, msg[:n])
		return false
	}

	return true
}

func telegramSendStatus(chatId int64) bool {
	var status ParticldStatus

	g_particldStatusMutex.Lock()
	status = g_particldStatus
	g_particldStatusMutex.Unlock()

	msg := fmt.Sprintf("Particl Node Info %s:\n", time.Now().UTC().Format(time.RFC3339))
	msg += fmt.Sprintf("```")
	msg += fmt.Sprintf("  Status    : %s\n", status.Status)
	msg += fmt.Sprintf("  Version   : %s\n", status.Version)
	msg += fmt.Sprintf("  Uptime    : %s\n", status.Uptime)
	msg += fmt.Sprintf("  Peers     : %s\n", status.Peers)
	msg += fmt.Sprintf("  Last Block: %s\n", status.LastBlock)
	msg += fmt.Sprintf("```")

	return telegramSendMessage(chatId, msg)
}

func telegramGetChat(chatName string) (bool, int64) {
	req := TGGetChat{chatName}
	var res TGChat

	if telegramCall(req, &res, "getChat", 10*time.Second) {
		return true, res.Id
	}

	return false, 0
}

func telegramBot() {
	updateOffset := 0
	for {
		var updateObj []TGUpdate

		if telegramCall(TGGetUpdate{updateOffset, 60}, &updateObj, "getUpdates", 70*time.Second) {
			for _, o := range updateObj {
				updateOffset = o.Update_id + 1

				m := o.Message

				if m.Date == 0 {
					m = o.Edited_message
				}

				if m.Date == 0 {
					continue
				}

				//fmt.Printf("TGUpate: ID: %d, text:%s\n", o.Update_id, m.Text)

				for _, u := range m.New_chat_members {
					if u.Username == g_tgConfig.BotName {
						fmt.Printf("TG: Bot added to chat %s(%d)\n", m.Chat.Title, m.Chat.Id)
					}
				}

				if m.Left_chat_member.Username == g_tgConfig.BotName {
					fmt.Printf("TG: Bot removed from chat %s(%d)\n", m.Chat.Title, m.Chat.Id)
				}

				user := m.From.First_name

				var cmd string
				var args []string

				for _, e := range m.Entities {
					if e.Type == "bot_command" {
						if e.Offset+e.Length <= len(m.Text) {
							cmd = m.Text[e.Offset : e.Offset+e.Length]
							if n := strings.Index(cmd, "@"); n >= 0 {
								cmd = cmd[:n]
							}
							args = strings.Split(strings.TrimSpace(m.Text[e.Offset+e.Length:]), " ")
						}
					}
				}

				if cmd != "" {
					fmt.Printf("TG cmd: %s, args: %s\n", cmd, strings.Join(args, ":"))
					switch cmd {
					case "/start":
						msg := fmt.Sprintf("Hello %s!", user)
						msg += fmt.Sprintf("\nThis bot is related to the Crymel Particl Cold Staking Pool https://particl.crymel.icu")
						msg += fmt.Sprintf("\n\nCommands:")
						msg += fmt.Sprintf("\n`  /status`: Print Staking Pool status")
						telegramSendMessage(m.Chat.Id, msg)

					case "/status":
						telegramSendStatus(m.Chat.Id)

					default:
						telegramSendMessage(m.Chat.Id, "Invalid command.")

					}
				}
			}
		} else {
			time.Sleep(2 * time.Second)
		}
	}
}

func telegramRegularMessages() {
	if g_tgConfig.StatusMsgHour < 0 || g_tgConfig.StatusMsgHour > 23 {
		fmt.Printf("TG: invalid status message hour value: %d", g_tgConfig.StatusMsgHour)
		return
	}
	if g_tgConfig.StatusMsgMinute < 0 || g_tgConfig.StatusMsgMinute > 59 {
		fmt.Printf("TG: invalid status message minute value: %d", g_tgConfig.StatusMsgMinute)
		return
	}
	if g_tgConfig.StatusMsgChatName == "" {
		fmt.Printf("TG: no chat name set.\n")
		return
	}

	ok, chatId := telegramGetChat(g_tgConfig.StatusMsgChatName)

	if !ok {
		fmt.Printf("TG: Failed to retrieve chat id for chat %s.", g_tgConfig.StatusMsgChatName)
		return
	}

	fmt.Printf("Sending Telegram status message at %02d:%02d UTC to chat %s(%d).\n", g_tgConfig.StatusMsgHour,
		g_tgConfig.StatusMsgMinute, g_tgConfig.StatusMsgChatName, chatId)

	latch := false
	for {
		now := time.Now().UTC()

		if now.Hour() == g_tgConfig.StatusMsgHour && now.Minute() == g_tgConfig.StatusMsgMinute {
			if !latch {
				telegramSendStatus(chatId)
				latch = true
			}
		} else {
			latch = false
		}

		time.Sleep(1 * time.Second)
	}
}

func signalHandler() {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGINT, syscall.SIGQUIT)

	s := <-signalChannel

	fmt.Printf("%s: Received Signal: %s\n", g_prgName, s.String())

	if err := g_httpServer.Shutdown(context.Background()); err != nil {
		// Error from closing listeners, or context timeout:
		fmt.Printf("HTTP server Shutdown: %v\n", err)
	}
}

func main() {

	fmt.Printf("Started %s\n", g_prgName)

	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Printf("Usage: %s <config file> [<telegram config file>]\n", g_prgName)
		os.Exit(1)
	}

	cfgFile := os.Args[1]

	if !readConfig(cfgFile) {
		fmt.Printf("%s: Failed to read config file.\n", g_prgName)
		os.Exit(1)
	}

	if len(os.Args) > 2 {
		if !readTelegramConfig(os.Args[2]) {
			fmt.Printf("%s: Failed to read Telegram config file.\n", g_prgName)
			os.Exit(1)
		}
	}

	go signalHandler()
	go particldStatusCollector()

	if g_tgConfig.BotName != "" && g_tgConfig.BotAuth != "" {
		go telegramBot()
		go telegramRegularMessages()
	}

	g_httpServer = &http.Server{
		Addr:           fmt.Sprintf("localhost:%d", g_config.Port),
		Handler:        nil,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	http.HandleFunc("/stat", handleDaemonStats)

	fmt.Println(g_httpServer.ListenAndServe())
}
