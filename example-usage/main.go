package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/FlxOne/logrus-redis-hook"
)

func init() {
	hook, err := logredis.NewHook("localhost", 6379, "my_redis_key", "v0", log.DebugLevel, false, 1000)
	if err == nil {
		log.AddHook(hook)
	} else {
		log.Error(err)
	}
}

func main() {
	// when hook is injected successfully, logs will be send to redis server
	log.Info("just some info logging...")

	log.WithFields(log.Fields{ "animal": "walrus", "number": 1, "size": 10, }).Info("and with fields")
}
