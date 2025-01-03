package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	tele "gopkg.in/telebot.v4"
)

type ResendTarget string

func (id ResendTarget) Recipient() string {
	return string(id)
}

type Source struct {
	Type string `koanf:"type" validate:"required,oneof='channel' 'group'"`
	Id   int64 `koanf:"id" validate:"required"`
}

type Config struct {
	Token  string `koanf:"token" validate:"required,len=46"`
	Resend struct {
		Enabled bool   `koanf:"enabled"`
		From    Source `koanf:"from" validate:"required_if=Enabled true"`
		To      Source `koanf:"to" validate:"required_if=Enabled true"`
	} `koanf:"resend"`
	Feedback struct {
		Enabled bool   `koanf:"enabled"`
		ChatId  string `koanf:"chatId" validate:"required_if=Enabled true"`
	} `koanf:"feedback"`
}

var (
	k        = koanf.New(".")
	validate = validator.New(validator.WithRequiredStructEnabled())
)

func loadConfig(config string) Config {
	if err := k.Load(rawbytes.Provider([]byte(config)), toml.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	var configData Config

	if err := k.Unmarshal("", &configData); err != nil {
		panic(err)
	}

	if err := validate.Struct(configData); err != nil {
		panic(err)
	}

	return configData
}

func main() {
	config := loadConfig(os.Getenv("APP_CONFIG"))

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK")
	})

	// Telegram bot setup
	pref := tele.Settings{
		Token:  config.Token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		panic(err)
	}

	b.Handle("/id", func (c tele.Context) error {
		id := strconv.Itoa(int(c.Chat().ID))
		log.Println("Got /id", id)
		return c.Send(strconv.Itoa(int(c.Chat().ID)))
	})

	if config.Resend.Enabled {
		log.Println("Enabling resend functionality")

		target := ResendTarget(strconv.Itoa(int(config.Resend.To.Id)))

		b.Handle("/forward", func(c tele.Context) error {
			log.Println("Got /forward", c.Chat().ID)

			if c.Message().IsReply() && c.Chat().ID == config.Resend.From.Id {
				_, err := b.Forward(target, c.Message().ReplyTo)
				return err
			}
			return nil
		})
	}

	b.SetCommands()

	// Starting the app
	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()

		log.Println("Starting HTTP server")
		log.Fatal(http.ListenAndServe(":3000", nil))
	}()

	go func() {
		defer wg.Done()

		log.Println("Starting Telegram bot")
		b.Start()
	}()

	wg.Wait()

	fmt.Println("a gde)")
}
