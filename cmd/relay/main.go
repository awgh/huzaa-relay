package main

import (
	"flag"
	"log"
	"os"

	"github.com/awgh/huzaa-relay/internal/config"
	"github.com/awgh/huzaa-relay/internal/turnrelay"
)

func main() {
	confPath := flag.String("config", "config/relay.json", "Path to relay config JSON")
	flag.Parse()

	cfg, err := config.LoadRelayConfig(*confPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	turnUsers := make([]turnrelay.TurnUserCred, 0, len(cfg.TurnUsers))
	for _, u := range cfg.TurnUsers {
		turnUsers = append(turnUsers, turnrelay.TurnUserCred{Username: u.Username, Secret: u.Secret})
	}
	relayCfg := &turnrelay.RelayConfig{
		TURNListen:  cfg.TURNListen,
		TURNSecret:  cfg.TURNSecret,
		TurnUsers:   turnUsers,
		DCCPortMin:  cfg.DCCPortMin,
		DCCPortMax:  cfg.DCCPortMax,
		RelayHost:   cfg.RelayHost,
		TLSCertFile: cfg.TLSCertFile,
		TLSKeyFile:  cfg.TLSKeyFile,
		MaxSessions: cfg.MaxSessions,
	}
	if relayCfg.DCCPortMin == 0 {
		relayCfg.DCCPortMin = 50000
		relayCfg.DCCPortMax = 50100
	}

	relay, err := turnrelay.NewRelay(relayCfg)
	if err != nil {
		log.Fatalf("new relay: %v", err)
	}
	if err := relay.Run(); err != nil {
		log.Fatalf("run relay: %v", err)
	}
	select {}
	os.Exit(0)
}
