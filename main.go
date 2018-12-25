package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
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

type Config struct {
	Port            int
	ParticldRpcPort int
	ParticldDataDir string
	DbUrl           string
}

var g_particldAuth = ""
var g_particldStatus ParticldStatus
var g_particldStatusMutex sync.Mutex
var g_config = Config{9100, 51735, "", ""}
var g_httpServer *http.Server

func readConfig(filename string) bool {
	data, err := ioutil.ReadFile(filename)

	if err != nil {
		log.Printf("Failed to open config file \"%s\": %s\n", filename, err.Error())
		return false
	}

	err = json.Unmarshal(data, &g_config)
	if err != nil {
		log.Printf("Syntax error in config file %s: %v", filename, err)
		return false
	}

	return true
}

func readParticldCookie() bool {
	path := fmt.Sprintf("%s/.cookie", g_config.ParticldDataDir)
	data, err := ioutil.ReadFile(path)

	if err != nil {
		log.Printf("Failed to read particld cookie file \"%s\": %s\n", path, err.Error())
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
		log.Printf("RPC: Marshal: %v", err)
		return false
	}
	resp, err := http.Post(addr, "application/json", strings.NewReader(string(data)))
	if err != nil {
		log.Printf("RPC: Post: %v", err)
		return false
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("RPC: ReadAll: %v", err)
		return false
	}
	//result := make(map[string]interface{})
	//log.Println(string(body))
	//log.Println(resp.Status)

	if resp.StatusCode != 200 {
		log.Printf("RPC: Bad response status: %s", resp.Status)
		return false
	}

	response := RpcResponse{}
	response.Result = res

	err = json.Unmarshal(body, &response)
	if err != nil {
		log.Printf("RPC: Unmarshal: %v", err)
		return false
	}

	//log.Printf("Result: Id: %d, error: %s\n", response.Id, response.Err)

	return true
}

func particldStatusCollector() {
	statusError := "error"
	na := "n/a"

	for {
		status := ParticldStatus{"OK", na, na, na, na, na}

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
			if execRpcJson(&stakeinfo, url, "getstakinginfo") {
				if stakeinfo.Staking {
					status.Staking = "enabled"
				} else {
					status.Staking = "disabled"
				}
			} else {
				status.Status = statusError
			}

			var uptime int64

			if execRpcJson(&uptime, url, "uptime") {
				status.Uptime = fmt.Sprintf("%.1f days", float64(uptime)/3600/24)
			} else {
				status.Status = statusError
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

	var status ParticldStatus

	g_particldStatusMutex.Lock()
	status = g_particldStatus
	g_particldStatusMutex.Unlock()

	data, err := json.Marshal(status)
	if err != nil {
		log.Printf("Marshal: %v", err)
	}
	io.WriteString(resp, string(data))

}

func signalHandler() {
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGINT)

	s := <-signalChannel

	log.Printf("staking_pool_statusd: Received Signal: %s", s.String())

	if err := g_httpServer.Shutdown(context.Background()); err != nil {
		// Error from closing listeners, or context timeout:
		log.Printf("HTTP server Shutdown: %v", err)
	}
}

func main() {

	log.Printf("Started staking_pool_statusd\n")

	if len(os.Args) != 2 {
		log.Printf("Usage: %s <config file>\n", os.Args[0])
	}

	cfgFile := os.Args[1]

	if !readConfig(cfgFile) {
		return
	}

	go signalHandler()
	go particldStatusCollector()

	g_httpServer = &http.Server{
		Addr:           fmt.Sprintf("localhost:%d", g_config.Port),
		Handler:        nil,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	http.HandleFunc("/stat", handleDaemonStats)

	log.Println(g_httpServer.ListenAndServe())

}
