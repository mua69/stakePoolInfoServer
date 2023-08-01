package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/go-gomail/gomail"
	_ "github.com/lib/pq"
	"github.com/mua69/particlrpc"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type ParticldStatus struct {
	Status            string  `json:"status"`
	Uptime            string  `json:"uptime"`
	Peers             string  `json:"peers"`
	LastBlock         string  `json:"last_block"`
	Version           string  `json:"version"`
	Weight            string  `json:"weight"`
	NetWeight         string  `json:"net_weight"`
	NominalRate       float64 `json:"nominal_rate"`
	ActualRate        float64 `json:"actual_rate"`
	SmsgFeeRateTarget float64 `json:"smsg_fee_rate_target"`
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

type PoolAccountInfo struct {
	Error         string `json:"error"`
	Accumulated   int64  `json:"accumulated"`
	Rewardpending int64  `json:"rewardpending"`
	Rewardpaidout int64  `json:"rewardpaidout"`
	Currenttotal  int64  `json:"currenttotal"`
	Laststaking   int64  `json:"laststaking"`
}

type Config struct {
	Port                  int
	ParticldRpcPort       int
	ParticldDataDir       string
	ParticldStakingWallet string
	ParticldStakingCtlKey string
	StakePoolUrl          string
	StakePoolRewardAdr    string
	ZmqEndpoint           string
	DbUrl                 string
	WatchdogEmailTo       string
	WatchdogEmailFrom     string
	WatchdogEmailSubject  string
	Smsgfeeratetarget     float64
}

type TGConfig struct {
	BotName             string
	BotAuth             string
	StatusMsgHour       int
	StatusMsgMinute     int
	StatusMsgChatName   string
	WatchdogMsgChatName string
}

type StakingRateHistory struct {
	Timestamp int64
	AvgRate   float64
	MinRate   float64
	MaxRate   float64
}

type Sat int64

const SatPerPart = 100000000
const BlocksPerYear = 365 * 24 * 30
const SecondsPerDay = 24 * 60 * 60

var g_prgName = "stakepoolInfoServer"
var g_particldStatus ParticldStatus
var g_particldStatusMutex sync.Mutex
var g_config = Config{Port: 0, ParticldRpcPort: 51735, ZmqEndpoint: "tcp://127.0.0.1:207922"}
var g_httpServer *http.Server
var g_tgConfig TGConfig
var g_TGBotEnabled = false

var g_stakingRateHistoryHourly []StakingRateHistory
var g_stakingRateHistoryDaily []StakingRateHistory

var g_db *sql.DB

func partToSat(v interface{}) Sat {
	fv, ok := v.(float64)
	if ok {
		return Sat(fv * SatPerPart)
	}

	return 0
}

func satToString(sat Sat) string {
	val := float64(sat)
	return fmt.Sprintf("%.8f PART", val/SatPerPart)
}

func readConfig(filename string) bool {
	data, err := ioutil.ReadFile(filename)

	if err != nil {
		fmt.Printf("Failed to open config file \"%s\": %s\n", filename, err.Error())
		return false
	}

	err = json.Unmarshal(data, &g_config)
	if err != nil {
		fmt.Printf("Syntax error in config file %s: %v\n", filename, err)
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
		fmt.Printf("Syntax error in Telegram config file %s: %v\n", filename, err)
		return false
	}

	return true
}

func dbConnect() *sql.DB {
	db, err := sql.Open("postgres", g_config.DbUrl)

	if err != nil {
		fmt.Printf("Cannot connect to data base: %v\n", err)
		return nil
	}

	err = db.Ping()

	if err != nil {
		fmt.Printf("Cannot connect to data base: %v\n", err)
		return nil
	}

	return db
}

// retieve JSON data from stakepool
func spCall(cmd string, res interface{}) bool {

	url := strings.TrimRight(g_config.StakePoolUrl, "/") + "/" + cmd

	resp, err := http.Get(url)

	if err != nil {
		fmt.Printf("stakepoolCall: Error: Get: %v", err)
		return false
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("stakepoolCall: Error: ReadAll: %v", err)
		return false
	}

	//fmt.Println(string(body))
	//fmt.Println(resp.Status)

	if resp.StatusCode != 200 {
		fmt.Printf("stakepoolCall: Bad response status: %s", resp.Status)
		return false
	}

	err = json.Unmarshal(body, &res)
	if err != nil {
		fmt.Printf("stakepoolCall: Unmarshal: %v", err)
		return false
	}

	return true
}

func spAccountInfo(account string, res *PoolAccountInfo) bool {
	cmd := "json/address/" + account
	return spCall(cmd, res)
}

func spConvertSatToString8(sat int64) string {
	val := float64(sat)
	return fmt.Sprintf("%.2f PART", val/SatPerPart)
}

func spConvertSatToString16(sat int64) string {
	val := float64(sat)
	return fmt.Sprintf("%.2f PART", val/SatPerPart/SatPerPart)
}

func particldStatusCollector() {
	statusError := "communication error"
	na := "n/a"

	prpc := particlrpc.NewParticlRpc()
	prpc.SetRpcPort(g_config.ParticldRpcPort)
	prpc.SetDataDirectoy(g_config.ParticldDataDir)

	for {

		status := ParticldStatus{Status:"", Version:na, Peers:na, LastBlock:na, Weight:na, NetWeight:na, Uptime:na}

		if err := prpc.ReadPartRpcCookie(); err == nil {

			nwinfo, err := prpc.GetNetworkInfo()
			if err == nil {
				status.Version = nwinfo.Subversion
				status.Peers = fmt.Sprintf("%d", nwinfo.Connections)
			} else {
				fmt.Println(err)
				status.Status = statusError
			}

			bcinfo, err := prpc.GetBlockchainInfo()
			if err == nil {
				status.LastBlock = fmt.Sprintf("%d", bcinfo.Blocks)
			} else {
				fmt.Println(err)
				status.Status = statusError
			}

			stakeinfo, err := prpc.GetStakingInfo(g_config.ParticldStakingWallet)
			if err == nil {
				status.Weight = fmt.Sprintf("%d PART", stakeinfo.Weight/SatPerPart)
				status.NetWeight = fmt.Sprintf("%dK PART", stakeinfo.Netstakeweight/SatPerPart/1000)

				if g_db == nil {
					// no db, calculate staking rate from stakeinfo
					calcStakingReward(stakeinfo)
				}
			} else {
				fmt.Println(err)
				status.Status = statusError
			}

			uptime, err := prpc.GetUptime()

			if err == nil {
				status.Uptime = fmt.Sprintf("%.1f days", float64(uptime)/3600/24)
			} else {
				fmt.Println(err)
				status.Status = statusError
			}

			stakingoptions, err := prpc.GetStakingOptions(g_config.ParticldStakingWallet)

			if err == nil {
				status.SmsgFeeRateTarget = stakingoptions.Smsgfeeratetarget

			} else {
				fmt.Println(err)
				status.Status = statusError
			}

			if status.Status != statusError {
				if stakeinfo.Staking {
					status.Status = "Staking"
				} else {
					status.Status = fmt.Sprintf("Not Staking: %s", stakeinfo.Errors)
				}
			}
		} else {
			status.Status = statusError
		}

		g_particldStatusMutex.Lock()

		g_particldStatus.Status = status.Status
		g_particldStatus.Uptime = status.Uptime
		g_particldStatus.Peers = status.Peers
		g_particldStatus.LastBlock = status.LastBlock
		g_particldStatus.Version = status.Version
		g_particldStatus.Weight = status.Weight
		g_particldStatus.NetWeight = status.NetWeight
		g_particldStatus.SmsgFeeRateTarget = status.SmsgFeeRateTarget

		g_particldStatusMutex.Unlock()

		time.Sleep(60 * time.Second)
	}
}

var g_avgActualReward = float64(0)

func calcStakingReward(stakeinfo *particlrpc.StakingInfo) {
	/*
		var blockReward float64

		blockReward = stakeinfo.Moneysupply * stakeinfo.Percentyearreward * (100 - stakeinfo.Foundationdonationpercent)
		blockReward /= 100 * 100
		blockReward /= BlocksPerYear

		stakingTime := float64(stakeinfo.Expectedtime) / SecondsPerDay

		actualReward := blockReward / stakingTime * 365 * 100 / float64(stakeinfo.Weight) * SatPerPart
	*/

	nominalReward := stakeinfo.Percentyearreward * (100 - stakeinfo.Treasurydonationpercent) / 100

	actualReward := stakeinfo.Moneysupply * stakeinfo.Percentyearreward * (100 - stakeinfo.Treasurydonationpercent)
	actualReward /= 100 * 100
	actualReward /= float64(stakeinfo.Netstakeweight) / SatPerPart
	actualReward *= 100

	if g_avgActualReward != 0 {
		g_avgActualReward = 0.99*g_avgActualReward + 0.01*actualReward
	} else {
		g_avgActualReward = actualReward
	}

	g_particldStatusMutex.Lock()
	g_particldStatus.NominalRate = nominalReward
	g_particldStatus.ActualRate = g_avgActualReward
	g_particldStatusMutex.Unlock()

	//fmt.Printf("Actual avg reward: %.8f\n", g_avgActualReward)
}

func stakingRewardCollector() {
	avgCnt := 100
	for {
		var nominalRate float64
		var avgActualRate float64

		rows, err := g_db.Query("SELECT block_nr,block_time,nominal_rate,actual_rate FROM stakingratestats ORDER BY block_nr DESC LIMIT $1", avgCnt)

		if err == nil {
			n := 0
			avgActualRate = 0

			for cont := rows.Next(); cont; cont = rows.Next() {
				var blockNr int
				var blockTime int64
				var actualRate float64
				var nom float64

				err = rows.Scan(&blockNr, &blockTime, &nom, &actualRate)
				if err == nil {
					if nominalRate == 0 {
						nominalRate = nom
					}
					avgActualRate += actualRate
					n++
				} else {
					fmt.Printf("db scan failed: %v\n", err)
				}

			}
			err = rows.Err()
			if err != nil {
				fmt.Printf("db next row failed: %v\n", err)
			}
			err = rows.Close()
			if err != nil {
				fmt.Printf("db close rows failed: %v\n", err)
			}

			if n > 0 {
				avgActualRate /= float64(n)
			}
		} else {
			fmt.Printf("db query failed: %v\n", err)
		}

		g_particldStatusMutex.Lock()
		g_particldStatus.NominalRate = nominalRate
		g_particldStatus.ActualRate = avgActualRate
		g_particldStatusMutex.Unlock()

		time.Sleep(60 * time.Second)
	}

	/*
		zmqContext, err := zmq4.NewContext()
		if err != nil {
			fmt.Printf("zmq context creation failed: %v\n", err)
			return
		}

		zmq, err := zmqContext.NewSocket(zmq4.SUB)
		if err != nil {
			fmt.Printf("zmq socket creation failed: %v\n", err)
			return
		}

		err = zmq.Connect(g_config.ZmqEndpoint)
		if err != nil {
			fmt.Printf("zmq connect failed: %v\n", err)
			return
		}

		zmq.SetSubscribe("hashblock")

		for {
			stakeinfo, err := g_prpc.GetStakingInfo(g_config.ParticldStakingWallet)
			if err == nil {
				calcStakingReward(stakeinfo)
			} else {
				fmt.Println(err)
			}

			msg, err := zmq.RecvMessageBytes(0)
			if err != nil {
				fmt.Printf("zmq receive failed: %v\n", err)
				time.Sleep(60 * time.Second)
			} else {
				fmt.Printf("stakingRewardCollector: Processing block: %s\n", hex.EncodeToString(msg[1]))
			}
		}
	*/

}

func getStakingRateHistory(interval int64, cnt int) []StakingRateHistory {
	rows, err := g_db.Query("select block_time/$1 as x, sum(actual_rate)/count(actual_rate), min(actual_rate), max(actual_rate) from stakingratestats  group by x order by x desc limit $2",
		interval, cnt)

	if err != nil {
		fmt.Printf("getStakingRateHistory: db query failed: %v\n", err)
		return nil
	}

	round := func(x float64) float64 { return math.Floor(x*100+0.5) / 100 }
	res := make([]StakingRateHistory, 0, cnt)

	i := 0
	for cont := rows.Next(); cont; cont = rows.Next() {
		var timestamp int64
		var avgRate, minRate, maxRate float64

		err = rows.Scan(&timestamp, &avgRate, &minRate, &maxRate)

		if err == nil {
			if i < cnt {
				res = append(res, StakingRateHistory{timestamp * interval, round(avgRate),
					round(minRate), round(maxRate)})
				i++
			}
		} else {
			fmt.Printf("getStakingRateHistory: db scan failed: %v\n", err)
			break
		}
	}

	err = rows.Err()
	if err != nil {
		fmt.Printf("getStakingRateHistory: db next row failed: %v\n", err)
	}
	err = rows.Close()
	if err != nil {
		fmt.Printf("getStakingRateHistory: db close rows failed: %v\n", err)
	}

	return res
}

func stakingRateHistoryCollector() {
	var lastUpdate time.Time

	for {
		if time.Since(lastUpdate) > 10*60*time.Second {
			//fmt.Printf("Updating staking rate history.\n")
			lastUpdate = time.Now()

			histHourly := getStakingRateHistory(60*60, 24)
			histDaily := getStakingRateHistory(24*60*60, 30)

			g_particldStatusMutex.Lock()
			g_stakingRateHistoryHourly = make([]StakingRateHistory, len(histHourly))
			g_stakingRateHistoryDaily = make([]StakingRateHistory, len(histDaily))
			copy(g_stakingRateHistoryHourly, histHourly)
			copy(g_stakingRateHistoryDaily, histDaily)
			g_particldStatusMutex.Unlock()
		}

		time.Sleep(time.Second)
	}

}

func telegramCall(in, out interface{}, request string, timeout time.Duration) bool {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", g_tgConfig.BotAuth, request)

	client := &http.Client{
		Timeout: timeout,
	}

	data, err := json.Marshal(in)
	if err != nil {
		fmt.Printf("telegramCall: Marshal: %v\n", err)
		return false
	}

	resp, err := client.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		fmt.Printf("telegramCall: Post: %v\n", err)
		return false
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("telegramCall: ReadAll: %v\n", err)
		return false
	}

	//fmt.Println(string(body))
	//fmt.Println(resp.Status)

	if resp.StatusCode != 200 {
		fmt.Printf("telegramCall: Bad response status: %s\n", resp.Status)
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
	req.Disable_notification = true
	req.Disable_web_page_preview = false

	var res TGMessage
	if !telegramCall(req, &res, "sendMessage", 10*time.Second) {
		n := 20
		if len(msg) < n {
			n = len(msg)
		}
		fmt.Printf("telegramSendMessage: failed: chat_id: %d, msg: \"%s\"\n", chatId, msg[:n])
		return false
	}

	return true
}

func telegramCmdStatus(chatId int64) bool {
	var status ParticldStatus

	g_particldStatusMutex.Lock()
	status = g_particldStatus
	g_particldStatusMutex.Unlock()

	msg := "*Particl Node Info*\n"
	msg += "```"
	msg += fmt.Sprintf(" Timestamp  : %s\n", time.Now().UTC().Format(time.RFC3339))
	msg += fmt.Sprintf(" Status     : %s\n", status.Status)
	msg += fmt.Sprintf(" Version    : %s\n", status.Version)
	msg += fmt.Sprintf(" Uptime     : %s\n", status.Uptime)
	msg += fmt.Sprintf(" Peers      : %s\n", status.Peers)
	msg += fmt.Sprintf(" Last Block : %s\n", status.LastBlock)
	msg += fmt.Sprintf(" Staking    : %s\n", status.Weight)
	msg += fmt.Sprintf(" NetStaking : %s\n", status.NetWeight)
	msg += fmt.Sprintf(" MP Fee Vote: %f PART\n", status.SmsgFeeRateTarget)
	msg += "```"

	return telegramSendMessage(chatId, msg)
}

func telegramCmdStart(chatId int64, user string) bool {
	msg := fmt.Sprintf("Hello %s!\n\n", user)
	msg += "This bot is intended to monitor and query the Crymel Particl Cold Staking Pool: https://particl.crymel.icu\n"
	msg += "\n*Commands:*\n"
	msg += "/status - Get Particl node status\n"
	msg += "/accountinfo <account id> - Get account balance in staking pool\n"
	msg += "/stakeinfo [<amount PART>] - Get staking interest rate info"
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

func telegramCmdAccountInfo(chatId int64, args []string) {

	if len(args) < 1 {
		telegramSendMessage(chatId, "Missing account ID argument.")
		return
	}

	account := args[0]

	var info PoolAccountInfo
	msg := ""

	if spAccountInfo(account, &info) {
		if info.Error == "Invalid address" {
			msg = "Account ID `" + account + "` is not valid."
		} else {
			msg = "Staking pool account info for `" + account + "`:\n" +
				"total rewards: " + spConvertSatToString16(info.Accumulated) +
				", confirmed payout: " + spConvertSatToString8(info.Rewardpaidout) +
				", unconfirmed payout: " + spConvertSatToString8(info.Rewardpending) +
				", open payout: " + spConvertSatToString8(info.Accumulated/SatPerPart-info.Rewardpaidout-info.Rewardpending) +
				", last staking weight: " + spConvertSatToString8(info.Currenttotal)
		}
	} else {
		msg = "Error while retrieving account information - try again later."
	}

	telegramSendMessage(chatId, msg)
}

func telegramCmdStakeInfo(chatId int64, args []string) {
	var amount float64

	if len(args) >= 1 {
		var err error
		amount, err = strconv.ParseFloat(args[0], 64)
		if err != nil {
			telegramSendMessage(chatId, "PART amount value \""+args[0]+"\" is not valid.")
			return
		}
	}

	g_particldStatusMutex.Lock()
	status := g_particldStatus
	g_particldStatusMutex.Unlock()

	msg := fmt.Sprintf("Nominal annual staking interest rate: %.1f\n", status.NominalRate)
	msg += fmt.Sprintf("Actual annual staking interest rate: %.1f\n", status.ActualRate)

	if amount > 0 {
		reward := amount * status.NominalRate / 100 / 365
		msg += fmt.Sprintf("Nominal daily reward for staking %.2f PART: %.2f PART\n", amount, reward)

		reward = amount * status.ActualRate / 100 / 365
		msg += fmt.Sprintf("Actual daily reward for staking %.2f PART: %.2f PART\n", amount, reward)
	}

	telegramSendMessage(chatId, msg)
}

/*
Bot Commands:

status - Get Particl node status
accountinfo <account id> - Get account balance in staking pool
stakeinfo [<amount part>] - Get staking interest info
*/
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

				//cleanup args
				var tmp []string
				for _, a := range args {
					if a != "" {
						tmp = append(tmp, a)
					}
				}
				args = tmp

				if cmd != "" {
					fmt.Printf("TG cmd: %s, args: %s\n", cmd, strings.Join(args, ":"))
					switch cmd {
					case "/start":
						telegramCmdStart(m.Chat.Id, user)

					case "/status":
						telegramCmdStatus(m.Chat.Id)

					case "/accountinfo":
						telegramCmdAccountInfo(m.Chat.Id, args)

					case "/stakeinfo":
						telegramCmdStakeInfo(m.Chat.Id, args)

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
		fmt.Printf("TG: invalid status message hour value: %d\n", g_tgConfig.StatusMsgHour)
		return
	}
	if g_tgConfig.StatusMsgMinute < 0 || g_tgConfig.StatusMsgMinute > 59 {
		fmt.Printf("TG: invalid status message minute value: %d\n", g_tgConfig.StatusMsgMinute)
		return
	}
	if g_tgConfig.StatusMsgChatName == "" {
		fmt.Printf("TG: no chat name set.\n")
		return
	}

	ok, chatId := telegramGetChat(g_tgConfig.StatusMsgChatName)

	if !ok {
		fmt.Printf("TG: Failed to retrieve chat id for chat %s.\n", g_tgConfig.StatusMsgChatName)
		return
	}

	fmt.Printf("Sending Telegram status message at %02d:%02d UTC to chat %s(%d).\n", g_tgConfig.StatusMsgHour,
		g_tgConfig.StatusMsgMinute, g_tgConfig.StatusMsgChatName, chatId)

	toTc := func(h, m int) int { return h*60 + m }
	timeToTc := func(t time.Time) int { return toTc(t.Hour(), t.Minute()) }

	triggerTc := toTc(g_tgConfig.StatusMsgHour, g_tgConfig.StatusMsgMinute)
	lastTc := -1

	for {
		now := timeToTc(time.Now().UTC())

		if now != lastTc {
			lastTc = now
			//fmt.Printf("timer: %d\n", now)
			if now == triggerTc {
				telegramCmdStatus(chatId)
			}
		}

		time.Sleep(1 * time.Second)
	}
}

func sendWatchdogEmail(msg string) {
	if g_config.WatchdogEmailTo != "" && g_config.WatchdogEmailFrom != "" {
		m := gomail.NewMessage()
		m.SetHeader("From", g_config.WatchdogEmailFrom)
		m.SetHeader("To", g_config.WatchdogEmailTo)
		if g_config.WatchdogEmailSubject == "" {
			m.SetHeader("Subject", "Particld Watchdog Alert")
		} else {
			m.SetHeader("Subject", g_config.WatchdogEmailSubject)
		}

		m.SetHeader("Importance", "high")
		m.SetBody("text/plain", msg)

		d := gomail.Dialer{Host: "localhost", Port: 25}
		if err := d.DialAndSend(m); err != nil {
			fmt.Printf("Particld Watchdog: Failed to send email: %s\n", err.Error())
		}
	}
}

func particldWatchdog() {
	msg := ""
	lastMsg := ""

	var chatId int64
	chatIdOk := false

	if g_TGBotEnabled && g_tgConfig.WatchdogMsgChatName != "" {
		chatIdOk, chatId = telegramGetChat(g_tgConfig.WatchdogMsgChatName)

		if !chatIdOk {
			fmt.Printf("TG: Failed to retrieve chat id for chat %s.\n", g_tgConfig.WatchdogMsgChatName)
		}
	}

	prpc := particlrpc.NewParticlRpc()
	prpc.SetRpcPort(g_config.ParticldRpcPort)
	prpc.SetDataDirectoy(g_config.ParticldDataDir)

	for {
		err := prpc.ReadPartRpcCookie()

		if err != nil {
			msg = "communication to particld failed."
			fmt.Printf("Particld Watchdog: failed to read particld cookie: %s\n", err.Error())
		} else {
			stakeinfo, err := prpc.GetStakingInfo(g_config.ParticldStakingWallet)

			if err != nil {
				msg = fmt.Sprintf("communication to particld failed.")
				fmt.Printf("Particld Watchdog: particld communication error: %s\n", err.Error())
			} else {
				if stakeinfo.Staking {
					msg = "normal operation"
				} else {
					msg = fmt.Sprintf("particld is not staking, cause: %s", stakeinfo.Errors)
				}
			}
		}

		if msg != lastMsg {
			lastMsg = msg
			fmt.Printf("Particld Watchdog: %s\n", msg)

			msg = fmt.Sprintf("Particld watchdog: %s\n", msg)

			if chatIdOk {
				telegramSendMessage(chatId, msg)
			}

			sendWatchdogEmail(msg)
		}

		time.Sleep(60 * time.Second)
	}
}

func signalHandler() {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM)

	s := <-signalChannel

	fmt.Printf("%s: Received Signal: %s\n", g_prgName, s.String())

	if g_httpServer != nil {
		if err := g_httpServer.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout:
			fmt.Printf("HTTP server Shutdown: %v\n", err)
		}
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
		fmt.Printf("Marshal: %v\n", err)
	}
	io.WriteString(resp, string(data))

}

func handleStakingRateHistoryHourly(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	var hist []StakingRateHistory

	g_particldStatusMutex.Lock()
	hist = make([]StakingRateHistory, len(g_stakingRateHistoryHourly))
	copy(hist, g_stakingRateHistoryHourly)
	g_particldStatusMutex.Unlock()

	data := make([][]interface{}, 0, len(hist))

	for i := len(hist) - 1; i >= 0; i-- {
		t := time.Unix(hist[i].Timestamp, 0)

		data = append(data, []interface{}{t.Hour(), hist[i].AvgRate})
	}

	d, err := json.Marshal(data)
	if err != nil {
		fmt.Printf("Marshal: %v", err)
	}
	io.WriteString(resp, string(d))

}

func handleStakingRateHistoryDaily(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	var hist []StakingRateHistory

	g_particldStatusMutex.Lock()
	hist = make([]StakingRateHistory, len(g_stakingRateHistoryDaily))
	copy(hist, g_stakingRateHistoryDaily)
	g_particldStatusMutex.Unlock()

	data := make([][]interface{}, 0, len(hist))

	for i := len(hist) - 1; i >= 0; i-- {
		t := time.Unix(hist[i].Timestamp, 0)

		data = append(data, []interface{}{t.Day(), hist[i].AvgRate})
	}

	d, err := json.Marshal(data)
	if err != nil {
		fmt.Printf("Marshal: %v", err)
	}
	io.WriteString(resp, string(d))

}

func handleStakingRateHistory(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	var histHourly []StakingRateHistory
	var histDaily []StakingRateHistory

	g_particldStatusMutex.Lock()
	histDaily = make([]StakingRateHistory, len(g_stakingRateHistoryDaily))
	histHourly = make([]StakingRateHistory, len(g_stakingRateHistoryHourly))
	copy(histDaily, g_stakingRateHistoryDaily)
	copy(histHourly, g_stakingRateHistoryHourly)
	g_particldStatusMutex.Unlock()

	res := struct {
		Daily  [][]interface{} `json:"daily"`
		Hourly [][]interface{} `json:"hourly"`
	}{}

	data := make([][]interface{}, 0, len(histDaily))

	for i := len(histDaily) - 1; i >= 0; i-- {
		//t := time.Unix(histDaily[i].Timestamp, 0).UTC()

		data = append(data, []interface{}{histDaily[i].Timestamp, histDaily[i].AvgRate, histDaily[i].MinRate, histDaily[i].MaxRate})
	}

	res.Daily = data

	data = make([][]interface{}, 0, len(histHourly))

	for i := len(histHourly) - 1; i >= 0; i-- {
		//t := time.Unix(histHourly[i].Timestamp, 0).UTC()

		data = append(data, []interface{}{histHourly[i].Timestamp, histHourly[i].AvgRate, histHourly[i].MinRate, histHourly[i].MaxRate})
	}

	res.Hourly = data

	d, err := json.Marshal(res)
	if err != nil {
		fmt.Printf("Marshal: %v\n", err)
	}
	io.WriteString(resp, string(d))

}

func stakingCtl(enabled bool) string {
	prpc := particlrpc.NewParticlRpc()
	prpc.SetRpcPort(g_config.ParticldRpcPort)
	prpc.SetDataDirectoy(g_config.ParticldDataDir)
	err := prpc.ReadPartRpcCookie()

	if err != nil {
		fmt.Printf("stakingCtl: Failed to read particld cookie.")
		return "communication failed"
	}

	options, err := prpc.SetStakingOptions(enabled, g_config.StakePoolRewardAdr, g_config.Smsgfeeratetarget,
		g_config.ParticldStakingWallet)

	if err != nil {
		fmt.Printf("stakingCtl: SetStakingOptions failed: %v", err)
		return "communication failed"
	}

	if options.Enabled != enabled || options.Rewardaddress != g_config.StakePoolRewardAdr {
		fmt.Printf("stakingCtl: setting target options failed: %+v\n", *options)
		return "setting failed"
	}

	return "ok"
}

func handleStakingOn(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	fmt.Printf("Staking Ctl: on\n")

	res := stakingCtl(true)

	d, err := json.Marshal(res)
	if err != nil {
		fmt.Printf("handleStakingOn: marshal: %v\n", err)
	}
	io.WriteString(resp, string(d))
}

func handleStakingOff(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.Header().Set("Access-Control-Allow-Origin", "*")

	fmt.Printf("Staking Ctl: off\n")

	res := stakingCtl(false)

	d, err := json.Marshal(res)
	if err != nil {
		fmt.Printf("handleStakingOff: marshal: %v\n", err)
	}
	io.WriteString(resp, string(d))
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

	if g_config.DbUrl != "" {
		g_db = dbConnect()

		if g_db == nil {
			os.Exit(1)
		}
	}

	go particldStatusCollector()

	if g_db != nil {
		go stakingRewardCollector()
		go stakingRateHistoryCollector()
	}

	if g_tgConfig.BotName != "" && g_tgConfig.BotAuth != "" {
		g_TGBotEnabled = true
		go telegramBot()
		go telegramRegularMessages()
	}

	if (g_TGBotEnabled && g_tgConfig.WatchdogMsgChatName != "") ||
		(g_config.WatchdogEmailFrom != "" && g_config.WatchdogEmailTo != "") {
		go particldWatchdog()
	}

	if g_config.Port > 0 {
		g_httpServer = &http.Server{
			Addr:           fmt.Sprintf("localhost:%d", g_config.Port),
			Handler:        nil,
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxHeaderBytes: 1 << 20,
		}

		http.HandleFunc("/stat", handleDaemonStats)
		http.HandleFunc("/stakingrate", handleStakingRateHistory)
		http.HandleFunc("/stakingrate/hourly", handleStakingRateHistoryHourly)
		http.HandleFunc("/stakingrate/daily", handleStakingRateHistoryDaily)

		if g_config.ParticldStakingCtlKey != "" {
			http.HandleFunc("/staking/"+g_config.ParticldStakingCtlKey+"/1", handleStakingOn)
			http.HandleFunc("/staking/"+g_config.ParticldStakingCtlKey+"/0", handleStakingOff)
		}

		go signalHandler()
		fmt.Println(g_httpServer.ListenAndServe())
	} else {
		signalHandler()
	}

	fmt.Printf("%s: Stopped.\n", g_prgName)
}
