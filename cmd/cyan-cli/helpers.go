package main

import (
	"context"
	"fmt"

	"github.com/cyanstack/cyanstack/internal/drivers/local"
)

func cleanup(ctx context.Context, driver *local.Driver, containerID string) {
	fmt.Printf("Cleaning up container %s...\n", containerID[:12])
	driver.Stop(ctx, containerID)
	driver.Remove(ctx, containerID)
}

func cleanupContainer(ctx context.Context, driver *local.Driver, containerID string) {
	if containerID == "" {
		return
	}
	short := containerID
	if len(containerID) > 12 {
		short = containerID[:12]
	}
	fmt.Printf("Removing container %s...\n", short)
	driver.Stop(ctx, containerID)
	driver.Remove(ctx, containerID)
}
