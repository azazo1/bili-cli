package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/azazo1/bilibili-cli/internal/cli"
)

func main() {
	app := cli.NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupts := make(chan os.Signal, 2)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	go func() {
		<-interrupts
		cancel()
		<-interrupts
		os.Exit(130)
	}()
	err := app.Execute(ctx)
	if err == nil {
		return
	}
	var exitErr *cli.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.Code)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

