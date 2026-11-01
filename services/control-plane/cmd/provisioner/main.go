package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/axyra/control-plane/internal/db"
	"github.com/axyra/control-plane/internal/provision"
)

func main() {
	connection := flag.String("connection", "", "Print a ready tenant's writer DSN to the local operator")
	flag.Parse()
	c, err := provision.NewCipher(os.Getenv("PROVISIONING_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	database, err := db.Connect(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("platform database unavailable")
	}
	defer database.Close()
	network, image, endpoint := os.Getenv("MANAGED_NETWORK"), os.Getenv("MANAGED_AGENT_IMAGE"), os.Getenv("ANALYTICS_URL")
	if network == "" || image == "" || endpoint == "" {
		log.Fatal("MANAGED_NETWORK, MANAGED_AGENT_IMAGE and ANALYTICS_URL are required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	w := provision.Worker{DB: database, Cipher: c, Network: network, AgentImage: image, AnalyticsURL: endpoint}
	if *connection != "" {
		dsn, err := w.WriterConnection(ctx, *connection)
		if err != nil {
			log.Fatal("Cannot retrieve writer connection; verify tenant readiness and encryption key")
		}
		fmt.Println(dsn)
		return
	}
	log.Println("[provisioner] waiting for managed database jobs")
	if err = w.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal("provisioner stopped; check database connectivity")
	}
}
