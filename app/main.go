// go-gradle-cache is a remote build cache server for Gradle.
// It supports both local filesystem and S3-compatible backends.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
