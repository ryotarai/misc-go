package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	command := flag.String("command", "", "Command to run")
	file := flag.String("file", "", "File to watch")
	waitStr := flag.String("wait", "1m", "Time to wait after last update")
	flag.Parse()
	if *command == "" {
		return fmt.Errorf("no command is specified")
	}
	if *file == "" {
		return fmt.Errorf("no file is specified")
	}

	waitDuration, err := time.ParseDuration(*waitStr)
	if err != nil {
		return err
	}

	var lastRunModTime time.Time
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if stat, err := os.Stat(*file); err == nil {
			if lastRunModTime.IsZero() {
				lastRunModTime = stat.ModTime()
			}

			diff := time.Now().Sub(stat.ModTime())
			if diff >= waitDuration && lastRunModTime != stat.ModTime() {
				cmd := exec.Command(*command)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					log.Printf("command failed: %s", err)
				}
				lastRunModTime = stat.ModTime()
			}
		} else {
			log.Printf("os.Stat failed: %s", err)
		}
		<-ticker.C
	}
}
