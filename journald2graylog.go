package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"

	kingpin "gopkg.in/alecthomas/kingpin.v2"

	"github.com/jan-zajic/journald2graylog/blacklist"
	"github.com/jan-zajic/journald2graylog/gelf"
	"github.com/jan-zajic/journald2graylog/journald"
	rkgelf "github.com/robertkowalski/graylog-golang"
)

var (
	verbose           = kingpin.Flag("verbose", "Wether journald2graylog will be verbose or not.").Short('v').Bool()
	enableRawLogLine  = kingpin.Flag("enable-rawlogline", "Wether journald2graylog will send the raw log line or not, disabled by default.").Envar("J2G_ENABLE_RAWLOGLINE").Bool()
	blacklistFlag     = kingpin.Flag("blacklist", "Prevent sending matching logs to the Graylog server. The value of this parameter can be one or more Regex separated by a space ( e.g. : \"foo.* bar.*\" )").Envar("J2G_BLACKLIST").String()
	graylogHostname   = kingpin.Flag("hostname", "Hostname or IP of your Graylog server, it has no default and MUST be specified").Envar("J2G_HOSTNAME").Required().String()
	nodeHostname      = kingpin.Flag("host", "Override host field in Gelf format. If not specified take hostname from journald or hostname of machine").Default("").Envar("J2G_HOST").String()
	graylogPort       = kingpin.Flag("port", "Port of the UDP GELF input of the Graylog server").Default("12201").Envar("J2G_PORT").Int()
	graylogPacketSize = kingpin.Flag("packet-size", "Maximum size of the TCP/IP packets you can use between the source (journald2graylg) and the destination (your Graylog server)").Default("1420").Envar("J2G_PACKET_SIZE").Int()
)

func main() {
	kingpin.Parse()

	if *verbose {
		log.Printf("Graylog host:\"%s\" port:\"%d\" packet size:\"%d\" blacklist:\"%v\" enableRawLogLine:\"%t\"",
			*graylogHostname, *graylogPort, *graylogPacketSize, strings.Split(*blacklistFlag, ";"), *enableRawLogLine)
	}

	// Determine what will be the default value of the "hostname" field in the
	// GELF payload.
	defaultHostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	if defaultHostname == "localhost" || defaultHostname == "" {
		defaultHostname = "Unknown Host"
	}

	// Build the object that will allow us to transmit messages to the Graylog
	// server.
	graylog := rkgelf.New(rkgelf.Config{
		GraylogHostname: *graylogHostname,
		GraylogPort:     *graylogPort,
		Connection:      "wan",
		MaxChunkSizeLan: *graylogPacketSize,
	})

	b := blacklist.PrepareBlacklist(blacklistFlag)

	// Build the go reader of stdin from where the log stream will be coming from.
	reader := bufio.NewReader(os.Stdin)

	// Loop and process entries from stdin until EOF.
	for {
		line, overflow, err := reader.ReadLine()
		if err == io.EOF {
			os.Exit(0)
		}
		if err != nil {
			panic(err)
		}
		if overflow {
			log.Println("Got a log line that was bigger than the allocated buffer, it will be skipped.")
			continue
		}
		if b.IsBlacklisted(line) {
			continue
		}

		gelfPayload := prepareGelfPayload(enableRawLogLine, line, defaultHostname, b)
		if gelfPayload == "" {
			continue
		}

		if *verbose {
			log.Println(gelfPayload)
		}

		err = graylog.Log(gelfPayload)
		if err != nil {
			panic(err)
		}
	}

}

func prepareGelfPayload(enableRawLogLine *bool, line []byte, defaultHostname string, blacklist blacklist.Blacklist) string {
	var logEntry journald.JournaldJSONLogEntry
	var gelfLogEntry gelf.GELFLogEntry

	err := json.Unmarshal(line, &logEntry)
	if err != nil {
		log.Printf("The following log line was not a correctly JSON encoded, it will be skiped: \"%s\"\n", line)
		fmt.Println(err)
		return ""
	}

	switch t := logEntry.MessageJSON.(type) {
	case string:
		logEntry.Message = logEntry.MessageJSON.(string)
		break
	case []interface{}:
		if len(t) > 0 {
			first := t[0]
			switch first.(type) {
			case float64:
				var bytes []byte
				for _, item := range t {
					bytes = append(bytes, byte(item.(float64)))
				}
				logEntry.Message = string(bytes)
			}
			break
		}
	default:
		log.Printf("The following log line was not a correctly JSON encoded, it will be skiped: \"%s\"\n", line)
		return ""
	}

	if len(blacklist.RegexpMap) > 0 {
		r := reflect.ValueOf(logEntry)
		for k, v := range blacklist.RegexpMap {
			f := reflect.Indirect(r).FieldByName(k)
			val := f.String()
			for _, regexp := range v {
				if regexp.MatchString(val) {
					//blacklisted entry, skipping
					return ""
				}
			}
		}
	}

	if *enableRawLogLine {
		gelfLogEntry.RawLogLine = string(line)
	}
	
	gelfLogEntry.Version = "1.1"
	if *nodeHostname != "" {
		gelfLogEntry.Host = *nodeHostname
	} else if logEntry.Hostname == "" || logEntry.Hostname == "localhost" {
		gelfLogEntry.Host = defaultHostname
	} else {
		gelfLogEntry.Host = logEntry.Hostname
	}
	gelfLogEntry.Level, err = strconv.Atoi(logEntry.Priority)
	if err != nil {
		panic(err)
	}
	gelfLogEntry.ShortMessage = logEntry.Message
	var jts = logEntry.RealtimeTimestamp
	gelfLogEntry.Timestamp, _ = strconv.ParseFloat(fmt.Sprintf("%s.%s", jts[:10], jts[10:]), 64)
	if (logEntry.SyslogFacility != "") && (logEntry.SyslogIdentifier != "") {
		gelfLogEntry.Facility = fmt.Sprintf("%s (%s)", logEntry.SyslogFacility, logEntry.SyslogIdentifier)
	} else if logEntry.SyslogFacility != "" {
		gelfLogEntry.Facility = logEntry.SyslogFacility
	} else if logEntry.SyslogIdentifier != "" {
		gelfLogEntry.Facility = logEntry.SyslogIdentifier
	} else {
		gelfLogEntry.Facility = "Undefined"
	}
	gelfLogEntry.BootID = logEntry.BootID
	gelfLogEntry.MachineID = logEntry.MachineID
	gelfLogEntry.PID = logEntry.PID
	gelfLogEntry.UID = logEntry.UID
	gelfLogEntry.GID = logEntry.GID
	gelfLogEntry.Executable = logEntry.Executable
	gelfLogEntry.CommandLine = logEntry.CommandLine
	var lineNumber int
	if logEntry.CodeLine != "" {
		lineNumber, err = strconv.Atoi(logEntry.CodeLine)
		if err != nil {
			return ""
		}
		// GELF: Line
		gelfLogEntry.Line = &lineNumber
		// GELF: File
		gelfLogEntry.File = logEntry.CodeFile
		// GELF: Function
		gelfLogEntry.Function = logEntry.CodeFunction
	}

	gelfLogEntry.ContainerID = logEntry.ContainerID
	gelfLogEntry.ContainerIDFull = logEntry.ContainerIDFull
	gelfLogEntry.ContainerName = logEntry.ContainerName
	gelfLogEntry.ContaierTag = logEntry.ContaierTag
	gelfLogEntry.ContainerPartialMessage = logEntry.ContainerPartialMessage

	gelfLogEntry.Transport = logEntry.Transport
	gelfPayloadBytes, err := json.Marshal(gelfLogEntry)
	if err != nil {
		panic(err)
	}
	gelfPayload := string(gelfPayloadBytes)
	return gelfPayload
}
