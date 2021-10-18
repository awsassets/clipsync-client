package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"strconv"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/lemon-mint/godotenv"
	"github.com/lemon-mint/vbox"
	"golang.design/x/clipboard"
)

var box *vbox.BlackBox
var lasttick int64

var DeviceGroup string

func roll() {
	if time.Now().Unix()/86400 != lasttick {
		lasttick = time.Now().Unix() / 86400
		box = vbox.NewBlackBox([]byte(os.Getenv("X_PSK") + strconv.FormatInt(lasttick, 10) + DeviceGroup))
	}
}

func Seal(data []byte) []byte {
	roll()
	return box.Seal(data)
}

func Open(data []byte) ([]byte, bool) {
	roll()
	return box.Open(data)
}

type Packet struct {
	Type    string `json:"type"`
	Sender  uint64 `json:"sender"`
	Content string `json:"content"`
}

var myID uint64 = uint64(time.Now().UnixNano())

func main() {
	godotenv.Load()

	opts := mqtt.NewClientOptions()
	opts.AddBroker(os.Getenv("X_MQTT_ENDPOINT"))
	opts.SetUsername(os.Getenv("X_MQTT_USERNAME"))
	opts.SetPassword(os.Getenv("X_MQTT_PASSWORD"))

	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(time.Second * 10)
	opts.OnConnect = func(c mqtt.Client) {
		log.Println("Connected to MQTT")
	}
	opts.OnConnectAttempt = func(broker *url.URL, tlsCfg *tls.Config) *tls.Config {
		log.Println("Connecting to MQTT")
		return tlsCfg
	}
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	infwait := make(chan bool)
	time.Sleep(time.Millisecond * 10)
	go func() {
		imgch := clipboard.Watch(context.TODO(), clipboard.FmtImage)
		textch := clipboard.Watch(context.TODO(), clipboard.FmtText)
		DeviceGroup = os.Getenv("X_DEVICE_GROUP")
		client.Subscribe(os.Getenv("X_DEVICE_GROUP"), 0, func(c mqtt.Client, m mqtt.Message) {
			log.Println("Received message:", len(m.Payload()))
			data, ok := Open(m.Payload())
			if !ok {
				return
			}
			pkt := Packet{}
			err := json.Unmarshal(data, &pkt)
			if err != nil {
				return
			}
			if pkt.Sender == myID {
				return
			}
			switch pkt.Type {
			case "text":
				body, err := base64.RawStdEncoding.DecodeString(pkt.Content)
				if err != nil {
					return
				}
				clipboard.Write(clipboard.FmtText, body)
			case "img":
				body, err := base64.RawStdEncoding.DecodeString(pkt.Content)
				if err != nil {
					return
				}
				clipboard.Write(clipboard.FmtImage, body)
			}
		})
		for {
			select {
			case img := <-imgch:
				log.Println("Image:", len(img))
				pkt := Packet{
					Type:    "img",
					Sender:  myID,
					Content: base64.RawStdEncoding.EncodeToString(img),
				}
				data, err := json.Marshal(pkt)
				if err != nil {
					continue
				}
				client.Publish(DeviceGroup, 0, false, Seal(data))
			case text := <-textch:
				log.Println("Text:", len(text))
				pkt := Packet{
					Type:    "text",
					Sender:  myID,
					Content: base64.RawStdEncoding.EncodeToString(text),
				}
				data, err := json.Marshal(pkt)
				if err != nil {
					continue
				}
				client.Publish(DeviceGroup, 0, false, Seal(data))
			}
		}
	}()
	<-infwait
}
