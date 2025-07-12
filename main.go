package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
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

type Command struct {
	Text        string `koanf:"text" validate:"required_if=Enabled true"`
	Description string `koanf:"description" validate:"required_if=Enabled true"`
}

type ResendConfig struct {
	Enabled  bool     `koanf:"enabled"`
	Command  Command  `koanf:"command" validate:"required_if=Enabled true"`
	Keywords []string `koanf:"keywords"`
	From     []int64  `koanf:"from" validate:"required_if=Enabled true"`
	To       []int64  `koanf:"to" validate:"required_if=Enabled true"`
}

type FeedbackConfig struct {
	Enabled bool    `koanf:"enabled"`
	To      []int64 `koanf:"to" validate:"required_if=Enabled true"`
}

type Config struct {
	Token    string         `koanf:"token" validate:"required,len=46"`
	Resend   ResendConfig   `koanf:"resend"`
	Feedback FeedbackConfig `koanf:"feedback"`
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

	var botCommands []tele.Command

	b.Handle("/id", func(c tele.Context) error {
		id := strconv.Itoa(int(c.Chat().ID))
		log.Println("Got /id", id)
		return c.Send(strconv.Itoa(int(c.Chat().ID)))
	})

	botCommands = append(botCommands, tele.Command{
		Text:        "/id",
		Description: "Get ID of this chat",
	})

	if config.Resend.Enabled {
		log.Println("Enabling resend functionality")

		resendHandler := func(c tele.Context) error {
			log.Println("Got resend", c.Chat().ID)

			if c.Message().IsReply() && slices.Contains(config.Resend.From, c.Chat().ID) {
				for _, toId := range config.Resend.To {
					target := ResendTarget(strconv.Itoa(int(toId)))
					log.Println("Resend", c.Chat().ID, target)

					if _, err := b.Forward(target, c.Message().ReplyTo); err != nil {
						log.Println("Error during resend", err)
					}
				}
			}
			return nil
		}

		// Listen to the registered command
		b.Handle(config.Resend.Command.Text, resendHandler)

		// Listen to the keywords
		for _, keyword := range config.Resend.Keywords {
			b.Handle(keyword, resendHandler)
		}

		botCommands = append(botCommands, tele.Command{
			Text:        config.Resend.Command.Text,
			Description: config.Resend.Command.Description,
		})
	}

	if config.Feedback.Enabled {
		log.Println("Enabling feedback functionality")

		forwardHandler := func(c tele.Context) error {
			if c.Chat().Type == tele.ChatPrivate {
				for _, toId := range config.Feedback.To {
					target := ResendTarget(strconv.Itoa(int(toId)))
					log.Println("Resend", c.Chat().ID, target)

					if _, err := b.Forward(target, c.Message()); err != nil {
						log.Println("Error during resend", err)
					}
				}
			}

			return nil
		}

		b.Handle(tele.OnPhoto, forwardHandler)
		b.Handle(tele.OnText, forwardHandler)
	}

	if err := b.SetCommands(botCommands); err != nil {
		log.Fatal(err)
	}

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
