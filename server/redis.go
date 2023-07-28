package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/redis/go-redis/v9"
	"os"
	"stomper"
	"strings"
)

var redisType = flag.String("redis-type", getEnvString("REDIS_TYPE", "single"), "redis connection type, (single|cluster|sentinel)")
var redisHost = flag.String("redis-host", getEnvString("REDIS_HOST", "localhost:6379"), "redis host, comma separated")
var redisUsername = flag.String("redis-username", getEnvString("REDIS_USERNAME", ""), "redis user, blank for no username")
var redisPassword = flag.String("redis-password", getEnvString("REDIS_PASSWORD", ""), "redis password, blank for no password")
var redisChannels = flag.String("redis-channels", getEnvString("REDIS_CHANNELS", "stomper"), "redis pub/sub channel(s) to subscribe to (defaults to 'stomper', list is separated with | e.g. 'stomper/a|stomper/b')")
var redisSentinelMasterName = flag.String("redis-sentinel-master-name", getEnvString("REDIS_SENTINEL_MASTER_NAME", ""), "redis sentinel master name (defaults to 'mymaster')")

func setupRedis(ctx context.Context, server *stomper.Server) {
	redisAddrs := strings.Split(*redisHost, ",")

	var subscription *redis.PubSub
	_redisType := strings.ToLower(*redisType)
	if _redisType == "single" {
		config := &redis.Options{
			Addr:       redisAddrs[0],
			ClientName: "stomper",
			MaxRetries: 5,
		}

		if *redisUsername != "" {
			config.Username = *redisUsername
		}

		if *redisPassword != "" {
			config.Password = *redisPassword
		}

		redisdb := redis.NewClient(config)
		if ping := redisdb.Ping(ctx); ping.Err() != nil {
			panic(ping)
		}

		channels := strings.Split(*redisChannels, "|")
		sugar.Infof("[redis] subscribing to (%s)", channels)
		subscription = redisdb.PSubscribe(ctx, channels...)
	} else if _redisType == "cluster" {
		config := &redis.ClusterOptions{
			Addrs:      redisAddrs,
			ClientName: "stomper",
			MaxRetries: 5,
		}

		if *redisUsername != "" {
			config.Username = *redisUsername
		}

		if *redisPassword != "" {
			config.Password = *redisPassword
		}

		redisdb := redis.NewClusterClient(config)
		if ping := redisdb.Ping(ctx); ping.Err() != nil {
			panic(ping)
		}

		channels := strings.Split(*redisChannels, "|")
		sugar.Infof("[redis] subscribing to (%s)", channels)
		subscription = redisdb.Subscribe(ctx, channels...)
	} else if _redisType == "sentinel" {
		config := &redis.FailoverOptions{
			SentinelAddrs: redisAddrs,
			MasterName:    *redisSentinelMasterName,
			ClientName:    "stomper",
			MaxRetries:    5,
		}

		if *redisUsername != "" {
			config.Username = *redisUsername
		}

		if *redisPassword != "" {
			config.Password = *redisPassword
		}

		redisdb := redis.NewFailoverClusterClient(config)
		if ping := redisdb.Ping(ctx); ping.Err() != nil {
			panic(ping)
		}

		channels := strings.Split(*redisChannels, "|")
		sugar.Infof("[redis] subscribing to (%s)", channels)
		subscription = redisdb.Subscribe(ctx, channels...)
	} else {
		sugar.Errorf("unknown redis type: %s", _redisType)
		os.Exit(1)
		return
	}

	go redisReceive(ctx, subscription, server)
}

type RedisMessage struct {
	Topic       string   `json:"topic"`
	Payload     []string `json:"payload"`
	ContentType string   `json:"contentType"`
}

func redisReceive(ctx context.Context, subscription *redis.PubSub, server *stomper.Server) {
	for {
		msg, err := subscription.ReceiveMessage(ctx)
		if err != nil {
			panic(err)
		}

		redisMessage := &RedisMessage{}
		err = json.Unmarshal([]byte(msg.Payload), &redisMessage)
		if err != nil {
			sugar.Errorf("unable to unmarshall: %v", err)
			continue
		}

		if redisMessage.ContentType == "" {
			redisMessage.ContentType = "application/json"
		}

		payload := fmt.Sprintf("[%s]", strings.Join(redisMessage.Payload, ","))

		server.SendMessage(
			fmt.Sprintf("/topic/%s", redisMessage.Topic),
			redisMessage.ContentType,
			payload,
		)
	}
}
