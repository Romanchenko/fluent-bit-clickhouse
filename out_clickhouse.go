package main

import (
	"C"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"sync"

	//"reflect"
	"time"
	"unsafe"

	"github.com/fluent/fluent-bit-go/output"
	//"github.com/ugorji/go/codec"
	"github.com/kshvakov/clickhouse"
	klog "k8s.io/klog"
)

var (
	client *sql.DB

	database  string
	table     string
	batchSize int

	insertSQL = "INSERT INTO %s.%s(date, cluster, namespace, app, pod_name, container_name, host, log, ts, rid, method, time, path, code) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	rw     sync.RWMutex
	buffer = make([]Log, 0)
)

const (
	DefaultWriteTimeout string = "20"
	DefaultReadTimeout  string = "10"

	DefaultBatchSize int = 1024
)

type Log struct {
	Cluster   string
	Namespace string
	App       string
	Pod       string
	Container string
	Host      string
	Log       string
	Ts        time.Time
	Rid       string
	Method    string
	Time      string
	Path      string
	Code      string
}

//export FLBPluginRegister
func FLBPluginRegister(ctx unsafe.Pointer) int {
	return output.FLBPluginRegister(ctx, "clickhouse", "Clickhouse Output Plugin.!")
}

//export FLBPluginInit
// ctx (context) pointer to fluentbit context (state/ c code)
func FLBPluginInit(ctx unsafe.Pointer) int {
	// init log
	//klog.InitFlags(nil)
	//flag.Set("stderrthreshold", "3")
	//flag.Parse()
	//
	//defer klog.Flush()

	// get config
	var host string
	if v := os.Getenv("CLICKHOUSE_HOST"); v != "" {
		host = v
	} else {
		klog.Error("you must set host of clickhouse!")
		return output.FLB_ERROR
	}

	var user string
	if v := os.Getenv("CLICKHOUSE_USER"); v != "" {
		user = v
	} else {
		klog.Error("you must set user of clickhouse!")
		return output.FLB_ERROR
	}

	var password string
	if v := os.Getenv("CLICKHOUSE_PASSWORD"); v != "" {
		password = v
	} else {
		klog.Error("you must set password of clickhouse!")
		return output.FLB_ERROR
	}

	if v := os.Getenv("CLICKHOUSE_DATABASE"); v != "" {
		database = v
	} else {
		klog.Error("you must set database of clickhouse!")
		return output.FLB_ERROR
	}

	if v := os.Getenv("CLICKHOUSE_TABLE"); v != "" {
		table = v
	} else {
		klog.Error("you must set table of clickhouse!")
		return output.FLB_ERROR
	}

	if v := os.Getenv("CLICKHOUSE_BATCH_SIZE"); v != "" {
		size, err := strconv.Atoi(v)
		if err != nil {
			klog.Infof("you set the default bacth_size: %d", DefaultBatchSize)
			batchSize = DefaultBatchSize
		}
		batchSize = size
	} else {
		klog.Infof("you set the default batch_size: %d", DefaultBatchSize)
		batchSize = DefaultBatchSize
	}

	var writeTimeout string
	if v := os.Getenv("CLICKHOUSE_WRITE_TIMEOUT"); v != "" {
		writeTimeout = v
	} else {
		klog.Infof("you set the default write_timeout: %s", DefaultWriteTimeout)
		writeTimeout = DefaultWriteTimeout
	}

	var readTimeout string
	if v := os.Getenv("CLICKHOUSE_READ_TIMEOUT"); v != "" {
		readTimeout = v
	} else {
		klog.Infof("you set the default read_timeout: %s", DefaultReadTimeout)
		readTimeout = DefaultReadTimeout
	}

	dsn := "tcp://" + host + "?username=" + user + "&password=" + password + "&database=" + database + "&write_timeout=" + writeTimeout + "&read_timeout=" + readTimeout

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		klog.Error("connecting to clickhouse: %v", err)
		return output.FLB_ERROR
	}

	if err := db.Ping(); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			klog.Errorf("[%d] %s \n%s\n", exception.Code, exception.Message, exception.StackTrace)
		} else {
			klog.Errorf("Failed to ping clickhouse: %v", err)
		}
		return output.FLB_ERROR
	}
	// ==
	client = db

	return output.FLB_OK
}

//export FLBPluginFlush
// FLBPluginFlush is called from fluent-bit when data need to be sent.
func FLBPluginFlush(data unsafe.Pointer, length C.int, tag *C.char) int {
	rw.Lock()
	defer rw.Unlock()

	// ping
	if err := client.Ping(); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			klog.Errorf("[%d] %s \n%s\n", exception.Code, exception.Message, exception.StackTrace)
		} else {
			klog.Errorf("Failed to ping clickhouse: %v", err)
		}
		return output.FLB_ERROR
	}

	// Create Fluent Bit decoder
	dec := output.NewDecoder(data, int(length))

	fillBuffer(dec)

	// sink data
	if len(buffer) < batchSize {
		klog.Infof("Buffer size is not enough for flushing: %d < %d", len(buffer), batchSize)
		return output.FLB_OK
	}

	result := flushToDatabase()
	return result
}

//export FLBPluginExit
func FLBPluginExit() int {
	klog.Infof("Buffer contains %d entries before exit", len(buffer))
	rw.Lock()
	defer rw.Unlock()
	if len(buffer) == 0 {
		return output.FLB_OK
	}

	// ping
	if err := client.Ping(); err != nil {
		if exception, ok := err.(*clickhouse.Exception); ok {
			klog.Errorf("[%d] %s \n%s\n", exception.Code, exception.Message, exception.StackTrace)
		} else {
			klog.Errorf("Failed to ping clickhouse: %v", err)
		}
		return output.FLB_ERROR
	}

	result := flushToDatabase()
	return result
}

func flushToDatabase() int {
	sql := fmt.Sprintf(insertSQL, database, table)

	start := time.Now()
	// post them to db all at once
	tx, err := client.Begin()
	if err != nil {
		klog.Errorf("begin transaction failure: %s", err.Error())
		return output.FLB_ERROR
	}

	// build statements
	smt, err := tx.Prepare(sql)
	if err != nil {
		klog.Errorf("prepare statement failure: %s", err.Error())
		return output.FLB_ERROR
	}
	for _, l := range buffer {
		// ensure tags are inserted in the same order each time
		// possibly/probably impacts indexing?
		_, err = smt.Exec(l.Ts, l.Cluster, l.Namespace, l.App, l.Pod, l.Container, l.Host,
			l.Log, l.Ts, l.Rid, l.Method, l.Time, l.Path, l.Code)

		if err != nil {
			klog.Errorf("statement exec failure: %s", err.Error())
			return output.FLB_ERROR
		}
	}

	// commit and record metrics
	if err = tx.Commit(); err != nil {
		klog.Errorf("commit failed failure: %s", err.Error())
		return output.FLB_ERROR
	}

	end := time.Now()
	klog.Infof("Exported %d log to clickhouse in %s", len(buffer), end.Sub(start))

	buffer = make([]Log, 0)
	return output.FLB_OK
}

func fillBuffer(decoder *output.FLBDecoder) {
	var ret int
	var timestampData interface{}
	var mapData map[interface{}]interface{}
	for {
		//// decode the msgpack data
		//err = dec.Decode(&m)
		//if err != nil {
		//	break
		//}

		ret, timestampData, mapData = output.GetRecord(decoder)
		if ret != 0 {
			break
		}

		// Get a slice and their two entries: timestamp and map
		//slice := reflect.ValueOf(m)
		//timestampData := slice.Index(0).Interface()
		//data := slice.Index(1)

		//timestamp, ok := timestampData.Interface().(uint64)
		//if !ok {
		//	klog.Errorf("Unable to convert timestamp: %+v", timestampData)
		//	return output.FLB_ERROR
		//}
		var timestamp time.Time
		switch t := timestampData.(type) {
		case output.FLBTime:
			timestamp = timestampData.(output.FLBTime).Time
		case uint64:
			timestamp = time.Unix(int64(t), 0)
		default:
			//klog.Infof("msg", "timestamp isn't known format. Use current time.")
			timestamp = time.Now()
		}

		// Convert slice data to a real map and iterate
		//mapData := data.Interface().(map[interface{}]interface{})
		flattenData, err := Flatten(mapData, "", UnderscoreStyle)
		if err != nil {
			break
		}

		log := Log{}
		for k, v := range flattenData {
			value := ""
			switch t := v.(type) {
			case string:
				value = t
			case []byte:
				value = string(t)
			default:
				value = fmt.Sprintf("%v", v)
			}

			switch k {
			case "cluster":
				log.Cluster = value
			case "kubernetes_namespace_name":
				log.Namespace = value
			case "kubernetes_labels_app":
				log.App = value
			case "kubernetes_labels_k8s-app":
				log.App = value
			case "kubernetes_pod_name":
				log.Pod = value
			case "kubernetes_container_name":
				log.Container = value
			case "kubernetes_host":
				log.Host = value
			case "log":
				ok, envoy_keys := parseEnvoy(value)
				if !ok {
					log.Log = value
				} else {
					for k_, v_ := range envoy_keys {
						switch k_ {
						case "time":
							log.Time = v_
						case "method":
							log.Method = v_
						case "path":
							log.Path = v_
						case "code":
							log.Code = v_
						case "x_request_id":
							log.Rid = v_
						}
					}
				}

				//case "response_flags":
				//	log.* = value
				//case "response_code_details":
				//	log.* = value
				//case "connection_termination_details":
				//	log.* = value
				//case "upstream_transport_failure_reason":
				//	log.* = value
				//case "bytes_received":
				//	log.* = value
				//case "bytes_sent":
				//	log.* = value
				//case "duration":
				//	log.* = value
				//case "x-envoy-upstream-service-time":
				//	log.* = value
				//case "x-forwarded-for":
				//	log.* = value
				//case "user-agent":
				//	log.* = value

				//case "authority":
				//	log.* = value
				//case "upstream_host":
				//	log.* = value
				//case "upstream_cluster":
				//	log.* = value
				//case "upstream_local_address":
				//	log.* = value
				//case "downstream_local_address":
				//	log.* = value
				//case "downstream_remote_address":
				//	log.* = value
				//case "requested_server_name":
				//	log.* = value
				//case "route_name":
				//	log.* = value
			}
		}

		if log.App == "" {
			break
		}

		//log.Ts = time.Unix(timestamp.Unix(), 0)
		log.Ts = timestamp
		buffer = append(buffer, log)
	}
}

func parseEnvoy(value string) (bool, map[string]string) {
	r := regexp.MustCompile(`\[(?P<time>[^\]]*)\] "(?P<method>\S+)(?: +(?P<path>[^\"]*?)(?: +\S*)?)(?: +?P<protocol>\S+)?" (?P<code>[^ ]*) (?P<response_flags>[^ ]*) (?P<response_code_details>[^ ]*) (?P<connection_termination_details>[^ ]*) "(?P<upstream_transport_failure_reason>[^ ]*)" (?P<bytes_received>[^ ]*) (?P<bytes_sent>[^ ]*) (?P<duration>[^ ]*) (?P<x_envoy_upstream_service_time>[^ ]*) "(?P<x_forwarded_for>[^ ]*)" "(?P<user_agent>[^\"]*)" "(?P<x_request_id>[^ ]*)" "(?P<authority>[^ ]*)" "(?P<upstream_host>[^ ]*)" (?P<upstream_cluster>[^ ]*) (?P<upstream_local_address>[^ ]*) (?P<downstream_local_address>[^ ]*) (?P<downstream_remote_address>[^ ]*) (?P<requested_server_name>[^ ]*) (?P<route_name>[^ ]*)`)
	matches := r.FindStringSubmatch(value)
	names := r.SubexpNames()
	for i, name := range r.SubexpNames() {
		fmt.Printf("%v: %v\n", name, matches[i])
	}
	if len(matches) != len(names) {
		return false, nil
	}
	result := make(map[string]string)
	for i, name := range names {
		result[name] = matches[i]
	}
	return true, result
}

func main() {
}
