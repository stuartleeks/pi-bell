package main

import (
	"fmt"
	"time"

	"github.com/stuartleeks/pi-bell/internal/pkg/gpio-components"
	"github.com/warthog618/gpiod"
)

const ChipName string = "gpiochip0"

func main() {

	fmt.Println("Loading")
	chip, err := gpiod.NewChip(ChipName)
	if err != nil {
		panic(err)
	}
	defer chip.Close()

	relay, err := gpio.NewRelay(chip, 18)
	if err != nil {
		panic(err)
	}

	fmt.Println("Off")
	err = relay.Off()
	if err != nil {
		panic(err)
	}

	time.Sleep(1 * time.Second)

	fmt.Println("On")
	err = relay.On()
	if err != nil {
		panic(err)
	}

	time.Sleep(1 * time.Second)

	fmt.Println("Off")
	err = relay.Off()
	if err != nil {
		panic(err)
	}

	time.Sleep(1 * time.Second)
}
