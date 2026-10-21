package config

import (
	"os"
	"strconv"
	"strings"
)

func dataTruckMCID() int {
	id, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("DATATRUCK_MC_ID")))
	return id
}
