package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime/debug"
	"sync"
	"time"

	dotenv "github.com/profclems/go-dotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	hue "github.com/stefanwichmann/go.hue"
	"golang.org/x/sync/errgroup"
)

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	err := dotenv.LoadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("dotenv.LoadConfig")
	}
}

var hueIP = dotenv.GetString("HUE_IP")
var hueUser = dotenv.GetString("HUE_USER")

var appName = func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		log.Fatal().Msg("debug.ReadBuildInfo")
	}
	return bi.Main.Path
}()

func main() {
	rand.Seed(time.Now().Unix())

	var bridge hue.Bridge

	if hueIP == "" {
		bridges, err := hue.DiscoverBridges(true)
		if err != nil {
			log.Fatal().Err(err).Msg("DiscoverBridges")
		}
		bridge = bridges[0]
	} else {
		b := hue.NewBridge(hueIP, hueUser)
		bridge = *b
	}

	for hueUser == "" {
		log.Info().Str("bridge", bridge.IpAddr).Msg("registering new device")
		bridge.Debug()
		err := bridge.CreateUser(appName)
		time.Sleep(time.Second)
		if err != nil {
			log.Warn().Err(err).Msg("CreateUser")
			time.Sleep(5 * time.Second)
			continue
		}
		log.Info().Str("username", bridge.Username).Str("ip", bridge.IpAddr).Msg("registered device")
		hueUser = bridge.Username
	}

	lights, _ := bridge.GetAllLights()

	ctx := context.Background()
	g, ctx := errgroup.WithContext(ctx)

	startTime := time.Now()

	newLights := make([]*hue.Light, 0)
	for _, l := range lights {
		if l.Name == "Dobbin Street" {
			continue
		}
		newLights = append(newLights, l)
	}
	lights = newLights

	shifter := make(chan int, len(lights)+1)
	shifter <- 20000
	shifter <- 20100
	shifter <- 20300
	// shifter <- 20900
	shifter <- 00000
	go func() {
		for {
			hue := rand.Intn(65536)
			select {
			case shifter <- hue:
				log.Debug().Msg("new hue")
			case h := <-shifter:
				shift := rand.Intn(500)
				shift -= 250
				// log.Debug().Msg("shift hue")
				shifter <- h + shift
			}
			time.Sleep(time.Millisecond)
		}
	}()

	var mutex sync.Mutex
	cond := sync.NewCond(&mutex)
	waitCount := 0

	for i, l := range lights {
		light := l
		idx := i

		g.Go(func() error {

			// log.Debug().Str("name", light.Name).Msg("light")
			attr, err := light.GetLightAttributes()
			if err != nil {
				return fmt.Errorf("light.GetLightAttributes: %w", err)
			}

			sls := copyState(attr.State)
			var sls2 hue.SetLightState

			defer func(light *hue.Light, sls hue.SetLightState) {
				sls.TransitionTime = "1"
				_, err := light.SetState(sls)
				log.Debug().Str("name", light.Name).Err(err).Msg("restore")

			}(light, sls)

			count := 0
			for time.Now().Sub(startTime) < 10*time.Second {
				time.Sleep((10 % time.Duration(count+idx+1)) * time.Millisecond)
				hue := <-shifter
				shifter <- hue
				sls2.Bri = "254"
				sls2.Hue = fmt.Sprintf("%d", hue)
				sls2.Sat = "254"
				sls2.TransitionTime = "1"
				sls2.On = "true"
				if (count+idx)%len(lights) == 0 {
					sls2.Bri = "254"
				} else {
					// sls2.TransitionTime = "1"
					// sls2.Bri = "100"
					// sls2.Sat = "100"
				}
				start := time.Now()
				_, err = light.SetState(sls2)
				dur := time.Now().Sub(start)

				log.Debug().Str("name", light.Name).Err(err).Dur("duration", dur).Int("count", count).Msg("run")
				time.Sleep(200 * time.Millisecond)
				count++

				mutex.Lock()
				waitCount++
				if waitCount == len(lights) {
					waitCount = 0
					cond.Broadcast()
					mutex.Unlock()
				} else {
					cond.Wait()
					mutex.Unlock()
				}

			}

			return nil
		})
	}
	err := g.Wait()
	log.Err(err).Send()
}

func copyState(s hue.LightState) hue.SetLightState {
	on := "false"
	if s.On {
		on = "true"
	}
	return hue.SetLightState{
		Alert:          s.Alert,
		Bri:            fmt.Sprintf("%d", s.Bri),
		Ct:             fmt.Sprintf("%d", s.Ct),
		Effect:         s.Effect,
		Hue:            fmt.Sprintf("%d", s.Hue),
		On:             on,
		Sat:            fmt.Sprintf("%d", s.Sat),
		TransitionTime: "0",
		Xy:             s.Xy,
	}
}
