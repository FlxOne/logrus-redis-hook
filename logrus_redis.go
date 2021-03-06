package logredis

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"github.com/sirupsen/logrus"
	"github.com/garyburd/redigo/redis"
)

// RedisHook to sends logs to Redis server
type RedisHook struct {
	RedisPool      *redis.Pool
	RedisHost      string
	RedisKey       string
	LogstashFormat string
	RedisPort      int
	Level          logrus.Level
	Async          bool
	EntryQueue     chan *logrus.Entry
	Quit           chan int
}

// LogstashMessageV0 represents v0 format
type LogstashMessageV0 struct {
	Type       string `json:"@type,omitempty"`
	Timestamp  string `json:"@timestamp"`
	Sourcehost string `json:"@source_host"`
	Message    string `json:"@message"`
	Level      string `json:"@level"`
	Fields     struct {
		File      string `json:"file"`
		Level     string `json:"level"`
		Timestamp string `json:"timestamp"`
	} `json:"@fields"`
	CustomFields map[string]string `json:"@custom_fields"`
}

// LogstashMessageV1 represents v1 format
type LogstashMessageV1 struct {
	Type       string `json:"@type,omitempty"`
	Timestamp  string `json:"@timestamp"`
	Sourcehost string `json:"host"`
	Message    string `json:"message"`
	Fields     struct {
		File      string `json:"file"`
		Level     string `json:"level"`
		Timestamp string `json:"timestamp"`
	} `json:"@fields"`
	CustomFields map[string]string `json:"@custom_fields"`
}

// NewHook creates a hook to be added to an instance of logger
func NewHook(host string, port int, key string, format string, level logrus.Level, async bool, bufferSize int) (*RedisHook, error) {
	pool := newRedisConnectionPool(host, port)

	// test if connection with REDIS can be established
	conn := pool.Get()
	defer conn.Close()

	// check connection
	_, err := conn.Do("PING")
	if err != nil {
		return nil, fmt.Errorf("unable to connect to REDIS: %s", err)
	}

	// by default, use V0 format
	if strings.ToLower(format) != "v0" && strings.ToLower(format) != "v1" {
		format = "v0"
	}

	redisHook := RedisHook {
		RedisHost:      host,
		RedisPool:      pool,
		RedisKey:       key,
		LogstashFormat: format,
		Level:          level,
		Async:          async,
		EntryQueue:     nil,
		Quit:           nil,
	}

	if async {
		redisHook.EntryQueue = make(chan *logrus.Entry, bufferSize)
		redisHook.Quit = make(chan int)
		go redisHook.asyncProcessing()
	}

	return &redisHook, nil
}

// Fire is called when a log event is fired.
func (hook *RedisHook) Fire(entry *logrus.Entry) error {
	if hook.Async {
		select {
		case hook.EntryQueue <- entry:
		default:
			fmt.Println("Buffer of redis hook's channel is full, log entry discarded")
		}

		return nil
	} else {
		return hook.processEntry(entry)
	}
}

// Levels returns the available logging levels.
func (hook *RedisHook) Levels() []logrus.Level {
	levels := make([]logrus.Level, 1)

	switch hook.Level {
	case logrus.DebugLevel:
		levels = append(levels, logrus.DebugLevel)
		fallthrough
	case logrus.InfoLevel:
		levels = append(levels, logrus.InfoLevel)
		fallthrough
	case logrus.WarnLevel:
		levels = append(levels, logrus.WarnLevel)
		fallthrough
	case logrus.ErrorLevel:
		levels = append(levels, logrus.ErrorLevel)
		fallthrough
	case logrus.FatalLevel:
		levels = append(levels, logrus.FatalLevel)
		fallthrough
	case logrus.PanicLevel:
		levels = append(levels, logrus.PanicLevel)
	}

	return levels
}

func (hook *RedisHook) asyncProcessing() {
	for {
		select {
		case entry := <- hook.EntryQueue:
			hook.processEntry(entry)
		case <- hook.Quit:
			return
		}
	}
}

func (hook *RedisHook) processEntry(entry *logrus.Entry) error {
	var msg interface{}

	switch hook.LogstashFormat {
	case "v0":
		msg = createV0Message(entry)
	case "v1":
		msg = createV1Message(entry)
	}

	js, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("error creating message for REDIS: %s", err)
	}

	conn := hook.RedisPool.Get()
	defer conn.Close()

	_, err = conn.Do("RPUSH", hook.RedisKey, js)
	if err != nil {
		return fmt.Errorf("error sending message to REDIS: %s", err)
	}
	return nil
}

func createV0Message(entry *logrus.Entry) LogstashMessageV0 {
	m := LogstashMessageV0{}
	m.Timestamp = entry.Time.UTC().Format(time.RFC3339Nano)
	m.Sourcehost = reportHostname()
	m.Message = entry.Message
	m.Fields.Level = entry.Level.String()
	m.CustomFields = logEntryToStringMap(entry)
	return m
}

func createV1Message(entry *logrus.Entry) LogstashMessageV1 {
	m := LogstashMessageV1{}
	m.Timestamp = entry.Time.UTC().Format(time.RFC3339Nano)
	m.Sourcehost = reportHostname()
	m.Message = entry.Message
	m.Fields.Level = entry.Level.String()
	m.CustomFields = logEntryToStringMap(entry)
	return m
}

func logEntryToStringMap(entry *logrus.Entry) map[string]string {
	m := make(map[string]string)

	if (len(entry.Data) > 0) {
		for key, value := range entry.Data {
			if str, ok := value.(string); ok {
				m[key] = str
			} else {
				m[key] = fmt.Sprintf("%v", value)
			}
		}
	}

	return m;
}

func newRedisConnectionPool(server string, port int) *redis.Pool {
	hostPort := fmt.Sprintf("%s:%d", server, port)
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", hostPort)
			if err != nil {
				return nil, err
			}

			// if password != "" {
			// 	if _, err := c.Do("AUTH", password); err != nil {
			// 		c.Close()
			// 		return nil, err
			// 	}
			// }

			return c, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

func reportHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
