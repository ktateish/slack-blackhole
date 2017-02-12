package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	logpkg "log"
	"os"
	"strconv"
	"time"

	"github.com/nlopes/slack"
)

var (
	log *logpkg.Logger

	API_READY    <-chan time.Time
	RTM          *slack.RTM
	CONFIG_BY_ID map[string]Config

	// flags
	CONFIG_FILE         string
	DEBUG               bool
	DEBUG_SLACK         bool
	DEFAULT_FILE_TTL    int
	DEFAULT_MESSAGE_TTL int
	DRY_RUN             bool
	SLACK_API_TOKEN     string
	SLACK_API_INTERVAL  int
)

func initLog() {
	log = logpkg.New(os.Stdout, "", logpkg.LstdFlags|logpkg.LUTC)
}

func debug(fmtstr string, args ...interface{}) {
	if !DEBUG {
		return
	}
	log.Printf("D: "+fmtstr, args...)
}

func info(fmtstr string, args ...interface{}) {
	log.Printf("I: "+fmtstr, args...)
}

func errorlog(fmtstr string, args ...interface{}) {
	log.Printf("E: "+fmtstr, args...)
}

func fatal(fmtstr string, args ...interface{}) {
	log.Fatalf("F: "+fmtstr, args...)
}

func jsonString(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

func initApiThrottle() {
	API_READY = time.NewTicker(time.Duration(SLACK_API_INTERVAL) * time.Second).C
}

func initSlackRTMClient() {
	slack.SetLogger(log)

	if SLACK_API_TOKEN == "" {
		fatal("BLACKHOLE_SLACK_API_TOKEN is not set")
	}
	debug("SLACK_API_TOKEN: %s", SLACK_API_TOKEN)
	api := slack.New(SLACK_API_TOKEN)
	if DEBUG_SLACK {
		api.SetDebug(true)
	}
	<-API_READY
	RTM = api.NewRTM()
	go RTM.ManageConnection()

	<-API_READY
	at, err := api.AuthTest()
	if err != nil {
		fatal("AuthTest failed: %v", err)
	}
	info("Connected to %s as %s", at.Team, at.User)
}

type Config struct {
	Channel    string `json:"channel"`
	MessageTTL int    `json:"message_ttl"`
	FileTTL    int    `json:"file_ttl"`
}

func initTTL() {
	if CONFIG_FILE == "" {
		info("CONFIG_FILE is not specified")
		return
	}
	f, err := os.Open(CONFIG_FILE)
	if err != nil {
		fatal("Open(%s) failed: %v", CONFIG_FILE, err)
	}
	data, err := ioutil.ReadAll(f)
	if err != nil {
		fatal("ReadAll failed: %v", err)
	}
	cfgs := []Config{}
	err = json.Unmarshal(data, &cfgs)
	if err != nil {
		fatal("Unmarshal(%s) failed: %v", CONFIG_FILE, err)
	}
	info("Config: %v", cfgs)

	channels, err := RTM.GetChannels(false)
	if err != nil {
		fatal("GetChannles failed: %v", err)
	}
	channelId := make(map[string]string)
	for _, ch := range channels {
		debug("channelId[%s]: %s", ch.Name, ch.ID)
		channelId[ch.Name] = ch.ID
	}
	for _, cfg := range cfgs {
		info("CONFIG_BY_ID[%s]: %v", channelId[cfg.Channel], cfg)
		CONFIG_BY_ID[channelId[cfg.Channel]] = cfg
	}
}

func unixTime(s string) (time.Time, error) {
	f, err := strconv.ParseFloat(s, 0)
	if err != nil {
		return time.Time{}, err
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1000000000)
	return time.Unix(sec, nsec), nil
}

func toBeDeleted(timeStamp string, ttl int) (time.Time, error) {
	ts, err := unixTime(timeStamp)
	if err != nil {
		return ts, err
	}
	return ts.Add(time.Duration(ttl) * time.Second), nil
}

func deleteMessage(ch string, msg *slack.Message, ttl int) {
	tbd, err := toBeDeleted(msg.Timestamp, ttl)
	if err != nil {
		errorlog("toBeDeleted() for message %s(%s) failed: %v", ch, msg.Timestamp, err)
		return
	}
	info("Message %s(%s) will be deleted at %v", ch, msg.Timestamp, tbd)
	go func() {
		<-time.After(tbd.Sub(time.Now()))
		if !DRY_RUN {
			<-API_READY
			RTM.DeleteMessage(ch, msg.Timestamp)
		}
		info("Deleted message: %s(%s)", ch, msg.Timestamp)
	}()
}

func handleMessage(ch string, msg *slack.Message) {
	info("Message: %s", jsonString(msg))
	if msg.SubType == "message_deleted" {
		// not a new message
		return
	}
	cfgttl := CONFIG_BY_ID[ch].MessageTTL
	ttl := DEFAULT_MESSAGE_TTL
	if cfgttl > 0 {
		ttl = cfgttl
	}
	debug("Message %s(%s): cfgttl..%d ttl..%d", ch, msg.Timestamp, cfgttl, ttl)
	if ttl > 0 {
		deleteMessage(ch, msg, ttl)
	}
}

func handleMessageEvent(msg *slack.MessageEvent) {
	info("MessageEvent: %s(%s)", msg.Channel, msg.Timestamp)
	m := slack.Message(*msg)
	handleMessage(msg.Channel, &m)
}

func deleteFile(file *slack.File, ttl int) {
	ts := file.Timestamp.Time()
	tbd := ts.Add(time.Duration(ttl) * time.Second)
	info("File %s (name='%s' title='%s') created %v (ttl=%d) will be deleted at %v", file.ID, file.Name, file.Title, ts, ttl, tbd)
	go func() {
		<-time.After(tbd.Sub(time.Now()))
		if !DRY_RUN {
			<-API_READY
			RTM.DeleteFile(file.ID)
		}
		info("Deleted File: id=%s name='%s' title='%s'", file.ID, file.Name, file.Title)
	}()
}

func handleFile(file *slack.File) {
	debug("handleFile: %s", jsonString(file))
	if len(file.Channels) == 0 {
		// file from File*Event doesn't have value in Channels field.
		// Re-get if so.
		<-API_READY
		f, _, _, err := RTM.GetFileInfo(file.ID, 0, 1)
		if err != nil {
			fatal("GetFileInfo for %s failed: %v", file.ID, err)
		}
		file = f
	}

	if len(file.Channels) != 1 {
		// file shared to multi channel is not supposed to be deleted
		info("File %s will not be deleted because of channel: %v", file.ID, file.Channels)
		return
	}
	ch := file.Channels[0]
	cfgttl := CONFIG_BY_ID[ch].FileTTL
	ttl := DEFAULT_FILE_TTL
	if cfgttl > 0 {
		ttl = cfgttl
	}
	if ttl > 0 {
		deleteFile(file, ttl)
	}
}

func handleFileCreated(file *slack.FileCreatedEvent) {
	info("File Created: %s", file.File.ID)
	handleFile(&file.File)
}

func handleFileShared(file *slack.FileSharedEvent) {
	info("File Shared: %s", file.File.ID)
	handleFile(&file.File)
}

func readEnv() {
	config_file := os.Getenv("BLACKHOLE_CONFIG_FILE")
	if config_file != "" {
		CONFIG_FILE = config_file
	}
	debug := os.Getenv("BLACKHOLE_DEBUG")
	if debug != "" {
		DEBUG = true
	}
	debug_slack := os.Getenv("BLACKHOLE_DEBUG_SLACK")
	if debug_slack != "" {
		DEBUG_SLACK = true
	}
	fileTTL := os.Getenv("BLACKHOLE_DEFAULT_FILE_TTL")
	if fileTTL != "" {
		val, err := strconv.ParseInt(fileTTL, 0, 0)
		if err != nil {
			errorlog("BLACKHOLE_DEFAULT_FILE_TTL=%s: ParseInt failed. Use default value(%d): %v", fileTTL, DEFAULT_FILE_TTL, err)
		} else {
			DEFAULT_FILE_TTL = int(val)
		}
	}
	messageTTL := os.Getenv("BLACKHOLE_DEFAULT_MESSAGE_TTL")
	if messageTTL != "" {
		val, err := strconv.ParseInt(messageTTL, 0, 0)
		if err != nil {
			errorlog("BLACKHOLE_DEFAULT_MESSAGE_TTL=%s: ParseInt failed. Use default value(%d): %v", messageTTL, DEFAULT_MESSAGE_TTL, err)
		} else {
			DEFAULT_MESSAGE_TTL = int(val)
		}
	}
	dry_run := os.Getenv("BLACKHOLE_DRY_RUN")
	if dry_run != "" {
		DRY_RUN = true
	}
	sai := os.Getenv("BLACKHOLE_SLACK_API_INTERVAL")
	if sai != "" {
		val, err := strconv.ParseInt(sai, 0, 0)
		if err != nil {
			errorlog("BLACKHOLE_SLACK_API_INTERVAL=%s: ParseInt failed. Use default value(%d): %v", sai, SLACK_API_INTERVAL, err)
		} else {
			SLACK_API_INTERVAL = int(val)
		}
	}
	token := os.Getenv("BLACKHOLE_SLACK_API_TOKEN")
	if token != "" {
		SLACK_API_TOKEN = token
	}
}

func inspectHistory(ch slack.Channel) {
	var err error
	h := &slack.History{HasMore: true}
	params := slack.NewHistoryParameters()
	for h.HasMore {
		<-API_READY
		h, err = RTM.GetChannelHistory(ch.ID, params)
		if err != nil {
			fatal("GetChannelHistory(%s, %v) failed: %v", ch.ID, params, err)
		}
		for i := 0; i < len(h.Messages); i++ {
			handleMessage(ch.ID, &h.Messages[i])
		}
		if len(h.Messages) > 0 {
			params.Latest = h.Messages[len(h.Messages)-1].Timestamp
		}
	}
}

func inspectFiles() {
	params := slack.NewGetFilesParameters()
	debug("NewGetFilesParameters: %v", params)
	for hasMore := true; hasMore; params.Page++ {
		files, paging, err := RTM.GetFiles(params)
		if err != nil {
			fatal("Failed to GetFiles(%v): %v", params, err)
		}
		for i := 0; i < len(files); i++ {
			handleFile(&files[i])
		}

		if paging.Page == paging.Pages {
			hasMore = false
		}
	}
}

func inspectPast() {
	<-API_READY
	channels, err := RTM.GetChannels(true)
	if err != nil {
		fatal("GetChannels() failed: %v", err)
	}
	info("There are %d channels", len(channels))
	for _, ch := range channels {
		if DEFAULT_MESSAGE_TTL == 0 && CONFIG_BY_ID[ch.ID].MessageTTL == 0 {
			continue
		}
		inspectHistory(ch)
	}

	inspectFiles()
}

func init() {
	initLog()
	flag.StringVar(&CONFIG_FILE, "config-file", "", "Configuration file")
	flag.BoolVar(&DEBUG, "debug", false, "Debug on")
	flag.BoolVar(&DEBUG_SLACK, "debug-slack", false, "Debug on for Slack")
	flag.IntVar(&DEFAULT_MESSAGE_TTL, "default-message-ttl", 0, "TTL of messages for all channel")
	flag.IntVar(&DEFAULT_FILE_TTL, "default-file-ttl", 0, "TTL of files for all channel")
	flag.BoolVar(&DRY_RUN, "dry-run", false, "Do not delete messages/files")
	flag.IntVar(&SLACK_API_INTERVAL, "slack-api-interval", 3, "Interval (sec) for api call")
	flag.StringVar(&SLACK_API_TOKEN, "slack-api-token", "", "Slack API token")
	readEnv()
	CONFIG_BY_ID = make(map[string]Config)
}

func main() {
	flag.Parse()
	initApiThrottle()
	initSlackRTMClient()
	initTTL()

	inspectPast()
	for msg := range RTM.IncomingEvents {
		switch ev := msg.Data.(type) {
		//case *slack.HelloEvent:
		case *slack.MessageEvent:
			handleMessageEvent(ev)
		case *slack.FileCreatedEvent:
			handleFileCreated(ev)
		case *slack.FileSharedEvent:
			handleFileShared(ev)
		default:
			debug("Event: %T %v", ev, ev)
		}
	}
}
