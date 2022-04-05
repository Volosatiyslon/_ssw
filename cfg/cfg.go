package cfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct{
	SrvName					string
	NumWorkers 				uint16
	NumTaskPerWorker 		uint16
	ProxyProviderUrl 		string
	TaskProviderUrl 		string
	TaskProviderQueue 		string
	AccProviderUrl 			string
	AccProviderToken 		string
	AccProviderType 		string
	PortalUrlUploadPath		string
	PortalResUploadPath 	string
	PortalAuth 				string
	ProtoResultConnStr 		string
	FileStFileUploadPath 	string
	FileStAuth 				string
	UrlPattern				string
	BadUrlRoute				string
	AccProviderTtl 			int
	UrlsEnrichment 			int
	ProxyProviderArg 		map[string]string
	ProxyRegion 			map[string]string
	AccProperties			map[string]string
	LoggingPath				string
	AppEnv					string
}

func (c *Config) Load() (error){
	loaded := 0
	for _, filename := range []string{".env", ".env.local"}{
		err := godotenv.Overload(filename)
		if err == nil{
			loaded++
		}
	}
	
	if loaded == 0{
		return errors.New("cant load env and env.local files")
	}
	if value, exists := os.LookupEnv("APP_ENV"); exists {
			filename := strings.Join([]string{".env.", value,".local"}, "")
			_ = godotenv.Overload(filename)
	}
	
	c.SrvName = getEnv("SRVNAME", "UnknownWrappedService")
	c.ProxyProviderUrl =  getEnv("PROXYPROVIDERURL", "")
	nw, err := strconv.Atoi(getEnv("NUMWORKERS", "0"))
	if err != nil {
		return fmt.Errorf("numworkers parse error: %v", err)
	}
	c.NumWorkers = uint16(nw)
	ntpw, err := strconv.Atoi(getEnv("NUMTASKPERWORKER", "0"))
	if err != nil {
		return fmt.Errorf("numtaskperworker parse error: %v", err)
	}
	c.NumTaskPerWorker =  uint16(ntpw)
	apt, err := strconv.Atoi(getEnv("ACCPROVIDERTTL", "0"))
	if err != nil {
		return fmt.Errorf("ttl parse error: %v", err)
	}
	c.AccProviderTtl = apt
	ue, err := strconv.Atoi(getEnv("URLSENRICHMENT", "0"))
	if err != nil {
		return fmt.Errorf("enrichement parse error: %v", err)
	}
	c.UrlsEnrichment = ue
	json.Unmarshal([]byte(getEnv("PROXYPROVIDERAG", "{}")),&c.ProxyProviderArg)
	json.Unmarshal([]byte(getEnv("PROXYREGION", "{}")),&c.ProxyRegion)
	json.Unmarshal([]byte(getEnv("ACCPROPERTY", "{}")),&c.AccProperties)
	c.ProxyProviderArg["app"] = c.SrvName
	c.ProxyProviderUrl =  getEnv("PROXYPROVIDERURL", "")
	c.TaskProviderUrl =  getEnv("TASKPROVIDERURL", "")
	c.TaskProviderQueue =  getEnv("TASKPROVIDERQUEUE", "")
	c.AccProviderUrl =  getEnv("ACCPROVIDERURL", "")
	c.AccProviderToken =  getEnv("ACCPROVIDERTOKEN", "")
	c.AccProviderType =  getEnv("ACCPROVIDERTYPE", "")
	c.PortalResUploadPath =  getEnv("PORTALRESUPLOADPATH", "")
	c.PortalUrlUploadPath =  getEnv("PORTALURLUPLOADPATH", "")
	c.PortalAuth =  getEnv("PORTALAUTH", "")
	c.ProtoResultConnStr =  getEnv("PROTORESULTCONNSTR", "")
	c.FileStFileUploadPath =  getEnv("FILESTFILEUPLOADPATH", "")
	c.FileStAuth =  getEnv("FILESTAUTH", "")
	c.LoggingPath = getEnv("LOGINGAPTH", "")
	c.AppEnv = getEnv("APPENV", "DEV")
	c.BadUrlRoute = getEnv("BADURLROUTE", "")
	c.UrlPattern = getEnv("URLPATTERN", "")
	return nil
}

func getEnv(key string, defaultVal string)string {
    if value, exists := os.LookupEnv(key); exists && value != "" {
			return value
    }

    return defaultVal
}

// func main(){
// 	var config Config
// 	err := config.Load()
// 	if err != nil{
// 		log.Fatalf("%w", err)
// 	}
// 	log.Printf("%+v", config)
// }