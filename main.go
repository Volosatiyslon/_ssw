package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	ampq "github.com/streadway/amqp"

	entities "privatrepo/bp/proto_go_lib"
	immortalampq "privatrepo/libs/rabbitImmortal"

	"google.golang.org/protobuf/types/known/timestamppb"

	"privatrepo/bp/collectorservices/search-service-wrapper/cfg"
)

type WorkerTask struct{
	Lifetime 	int64
	Task 		*Task
	Rules		[]string
	Account		*JAccount
	Region		map[string]string
}

// Globals
	
func main(){
	
	if len(os.Args[1:]) == 0{
		log.Panic("you miss arguments, exiting")
	}
	cmnd := os.Args[1]
	cmnd_args := os.Args[2:]
	conf := cfg.Config{}
	err := conf.Load()
	if err != nil{
		log.Printf("cant load config: %v, exiting", err)
	}

	// pp_config, _ := json.MarshalIndent(conf, "", " ")
	// log.Printf("current config:\n %s\n", pp_config)
	if e := strings.ToUpper(conf.AppEnv);e == "PROD"{
		f, err := os.OpenFile("trace.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
		if err != nil{
			log.Printf("can't write to \"trace.log\": %v", err)
			Suicide()
		}
		log.SetOutput(f)
	}
	mchann, err := NewTaskProvider(conf.TaskProviderUrl, conf.TaskProviderQueue, conf.NumWorkers*conf.NumTaskPerWorker)
	if err != nil{
		log.Printf("cant connect to task provider: %v", err)
		Suicide()
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	workerTaskList := make(map[string]*WorkerTask)
	stdin := make(chan []byte)
	resultsChan := make(chan JTaskResult)
	eventsChan := make(chan JTaskResult)
	outChans := make([]chan []byte, conf.NumWorkers)
	consumerErr := make(chan error)
	for i := uint16(0); i < conf.NumWorkers; i++{
		outchannel := make(chan []byte)
		outChans[i] = outchannel
		go consume(consumerErr, stdin, outchannel, cmnd,  cmnd_args...)
		log.Print("consumer started")
	}
	go func(){
		p := <- consumerErr
		log.Printf("stop working: %v, exiting", p)
		Suicide()
   }()
	accP := AccStor{}
	holder := AccHolder{}
	if conf.AccProviderUrl != ""{
		accP.Connect(conf.AccProviderUrl)
		holder.Create(int(conf.NumWorkers), conf.AccProviderTtl, &accP, conf.AccProviderToken)
		go holder.Hold()
	}
	protoP := ProtoStor{}
	if conf.ProtoResultConnStr != ""{
		protoP.Connect(conf.ProtoResultConnStr)
		log.Print("gRPC reciver started")
	}
	logger := LogEndp{}
	if conf.LoggingPath != ""{
		logger.Connect(conf.LoggingPath, conf.SrvName)
		log.Print("Log reciver started")
	}
	var filter regexp.Regexp
	var badUrlRmq *immortalampq.Channel
	var badUrlExch string
	var BadUrlRoute string
	filterEnabled := false
	if conf.UrlPattern != "" && conf.BadUrlRoute != ""{
		filterEnabled = true
		rmqConf := strings.Split(conf.BadUrlRoute, ";")
		conn, err := immortalampq.Dial(rmqConf[0], func (e error) bool {log.Print(e);return true}, func(){})
		if err != nil {
			log.Printf("cant dial to RMQ: %v", err)
			Suicide()
		}
		badUrlRmq, err = conn.Channel(func (e error) bool {log.Print(e);return true}, func(){})
		if err != nil {
			log.Printf("cant connect to RMQ: %v", err)
			Suicide()
		}
		switch len(rmqConf){
		case 2:
			badUrlExch = rmqConf[1]
			BadUrlRoute = ""
		case 3:
			badUrlExch = rmqConf[1]
			BadUrlRoute = rmqConf[2]
		default:
			log.Printf("wrong conf.BadUrlRoute string in env")
			Suicide()
		}
	}
	go resultsProduser(conf, resultsChan, workerTaskList, protoP, logger)
	go mixout(resultsChan, eventsChan, outChans...)
	go eventProduser(conf, eventsChan, stdin, workerTaskList, &accP, &holder)
	go watchDog(workerTaskList)
	
	
	for t := range(*mchann){
		var task JTask
		json.Unmarshal([]byte(t.GetBody()), &task)
		task.Id = t.GetId()
		log.Print("got task")
		if filterEnabled && task.Uri != "" && !filter.Match([]byte(task.Uri)){
			badUrlRmq.Publish(badUrlExch, BadUrlRoute, false, false, ampq.Publishing{Body: []byte(t.GetBody())}, func (e error) bool {return true})
			continue
		}
		wt :=  WorkerTask{
			Task: &t,
			Lifetime: time.Now().Unix(),
			Rules: task.RuleIDs,
		}
		var acc_region map[string]string
		if conf.AccProviderUrl != ""{
			var acc JAccount
			var err error
			accSearchProperties := conf.AccProperties
			if task.CountryCode != ""{
				accSearchProperties["countryCode"] = task.CountryCode
			}
			acc, acc_region, err = accP.GetAcc(conf.AccProviderType, accSearchProperties, int32(conf.AccProviderTtl), false)
			if err != nil{
				log.Printf("err during getting account: %v", err)
				Suicide()
			}
			accId := acc.AccId
			task.Login = acc.Login
			task.Passwd = acc.Passwd
			task.Session = acc.Session
			task.Proxy = acc.AccProxy
			wt.Account = &acc
			holder.Add(accId)
		}
		switch {
			case acc_region != nil:
				wt.Region = acc_region
			case task.CountryCode != "":
				wt.Region = map[string]string{
					"countryCode": task.CountryCode,
				}
			case conf.ProxyRegion != nil:
				wt.Region = conf.ProxyRegion

		}
		if conf.ProxyProviderUrl != "" && task.Proxy == ""{
			p, reg, err := GetProxy(conf.ProxyProviderUrl, conf.ProxyProviderArg, wt.Region)
			if err != nil{
				log.Printf("err during getting proxy: %v", err)
				Suicide()
			}
			if v,ok := reg["CountryCode"]; ok{
				task.CountryCode = v
			}
			if v,ok := reg["City"]; ok{
				task.City = v
			}
			task.Proxy = p
			wt.Region = reg
		}
		workerTaskList[t.GetId()] = &wt
		input, err := json.Marshal(task)
		if err != nil{
			log.Printf("err during writing task: %v", err)
			Suicide()
		}
		stdin <- input
		log.Printf("task %v writed", task.Id)
	}
	wg.Wait()
}

func mixout(resChan chan JTaskResult, eventChan chan JTaskResult, stdouts ...chan []byte){
	log.Print("mixer started")
	for _,outchan := range stdouts{
		go func(c chan []byte){
			for{
				output:= <- c
				var res JTaskResult
				err:= json.Unmarshal(output, &res)
				if err != nil{
					log.Printf("unexpected res: %s", output)
					Suicide()
				}
				if res.Unexpected != ""{
					log.Printf("worker got unexpected err: %v", res.Unexpected)
					Suicide()
				}
				if res.SearchResults != nil || res.Urls != nil || res.Proto != nil {
					log.Printf("... [%v] looks like result, sending to resultproduser", res.Id)
					resChan <- res
					continue
				}
				if res.Error != "" || res.Event != ""{
					log.Printf("... [%v] looks like event, sending to eventproduser", res.Id)
					eventChan <- res
					continue
				}
				log.Printf("unexpected output from worker: %+v", res)
				Suicide()
			}
		}(outchan)
	}
}

func watchDog(tasklist map[string]*WorkerTask) (){
	for{
		for k,v := range tasklist{
			startTime := v.Lifetime
			currentTime := time.Now().Unix()
			if currentTime-startTime > 240{
				log.Printf("task %v seems to be stuck", k)
			}
			if currentTime-startTime > 300{
				log.Printf("task %v is stuck, exiting", k)
				Suicide()
			}
		}
		time.Sleep(time.Duration(time.Second)*time.Duration(10))
	}
}

func eventProduser(conf cfg.Config, eventChan chan JTaskResult, stdin chan []byte,  tasklist map[string]*WorkerTask, storage *AccStor, holder *AccHolder){
	log.Print("event produser started")
	for{
		event:= <- eventChan
		if strings.ToLower(event.Error) == "connectionerror"{
			
			newP, reg, err := GetProxy(conf.ProxyProviderUrl, conf.ProxyProviderArg, tasklist[event.Id].Region)
			if err != nil {
				log.Printf("cant renew proxy: %v", err)
				Suicide()
			}
			event.Proxy = newP
			if v,ok := reg["CountryCode"]; ok{
				event.CountryCode = v
			}
			if v,ok := reg["City"]; ok{
				event.City = v
			}
			tasklist[event.Id].Lifetime = time.Now().Unix()
			event.Error = ""
			input, _ := json.Marshal(event)
			log.Printf("connection for %v renewed, sending back", event.Id)
			stdin <- input
			continue
		}
		if strings.ToLower(event.Event) == "accountextend"&&tasklist[event.Id].Account.Session != event.Session {
			tasklist[event.Id].Account.Session = event.Session
			err := storage.ExtendAcc((*tasklist[event.Id].Account), int32(conf.AccProviderTtl), event.Session)
			if err != nil{
				log.Printf("acc extend return: %v, exiting", err)
				Suicide()
			}
			log.Printf("account for %v updated, sending back", event.Id)
			event.Event = ""
			input, _ := json.Marshal(event)
			stdin <- input
			continue
		}
		if strings.ToLower(event.Event) == "accountrelease"{
			if tasklist[event.Id].Account.Session != event.Session {
				tasklist[event.Id].Account.Session = event.Session
				err := storage.ExtendAcc((*tasklist[event.Id].Account), int32(conf.AccProviderTtl), event.Session)
				if err != nil{
					log.Printf("acc session update return: %v, exiting", err)
					Suicide()
				}
			}
			err := holder.Del((*tasklist[event.Id].Account).AccId)
			if err != nil{
				log.Printf("acc account release return: %v, exiting", err)
				Suicide()
			}
			log.Printf("account for %v extended, sending back", event.Id)
			continue
		}
		if strings.ToLower(event.Event) == "accounterror"{
			err := holder.Del((*tasklist[event.Id].Account).AccId)
			if err != nil{
				log.Printf("acc account release (during blocking) return: %v, exiting", err)
				Suicide()
			}
			err = storage.BlockAccount((*tasklist[event.Id].Account))
			if err != nil{
				log.Printf("acc account block return: %v, exiting", err)
				Suicide()
			}
			acc, _, err := storage.GetAcc(conf.AccProviderType, conf.AccProperties, int32(conf.AccProviderTtl), true)
			if err != nil{
				log.Printf("err during getting account: %v", err)
				Suicide()
			}
			accId := acc.AccId
			event.Login = acc.Login
			event.Passwd = acc.Passwd
			event.Session = acc.Session
			event.Proxy = acc.AccProxy
			tasklist[event.Id].Account = &acc
			holder.Add(accId)
			if event.Proxy ==""	{
				newP, _, err := GetProxy(conf.ProxyProviderUrl, conf.ProxyProviderArg, tasklist[event.Id].Region)
				if err != nil {
					log.Printf("cant renew proxy: %v", err)
					Suicide()
				}
				event.Proxy = newP
			}
			tasklist[event.Id].Lifetime = time.Now().Unix()
			event.Error = ""
			log.Printf("account for %v renewed, sending back", event.Id)
			input, _ := json.Marshal(event)
			stdin <- input
			continue
		}
		if strings.ToLower(event.Event) == "accountfreeze"{
			if tasklist[event.Id].Account.Session != event.Session {
				tasklist[event.Id].Account.Session = event.Session
				err := storage.ExtendAcc((*tasklist[event.Id].Account),86400, event.Session)
				if err != nil{
					log.Printf("account freeze return: %v, exiting", err)
					Suicide()
				}
			}
			event.Event = ""
			log.Printf("account %v renewed, sending back", event.Id)
			input, _ := json.Marshal(event)
			stdin <- input
			continue
		}
		

		log.Printf("unsupported event can't produse(event field: %v, error field: %v)", event.Event, event.Error)		
		Suicide()
	}
}

func resultsProduser(conf cfg.Config, resultChan chan JTaskResult,  tasklist map[string]*WorkerTask, protoP ProtoStor, logger LogEndp){
	log.Print("result produser started")
	for{
		postingOk := false
		result:= <- resultChan
		logmess := entities.Log{
			Stage: "results posting",
			Result: &entities.Logresult{},
			Time: timestamppb.Now(),
		}
		if result.Urls != nil {
			err := UploadUrls(conf.PortalUrlUploadPath, result.Urls, tasklist[result.Id].Rules, conf.PortalAuth, conf.UrlsEnrichment)
			if err != nil{
				logmess.Result.Error = fmt.Sprintf("urls for %v not posted: %v", result.Id, err)
				logmess.Result.Status = 3
				logger.Send(&logmess)
				log.Printf("urls for %v not posted: %v", result.Id, err)
				Suicide()
			}
			urls := make([]string, len(result.Urls))
			for i, v := range(result.Urls){
				urls[i] = v.Url
			}
			logmess.SearchTask = &entities.LogSearchTask{
				RuleIds: result.RuleIDs,
				SearchWords: result.SearchWords,
				CountryCode: tasklist[result.Id].Region["countryCode"],
			}
			logmess.Result.Array = urls
			logmess.Result.Status = 1
			logger.Send(&logmess)
			log.Printf("urls for %v uploaded", result.Id)
			postingOk = true
			
		}
		if result.SearchResults != nil{
			for _, searchResult := range(result.SearchResults){
				err := UploadFiles(conf.FileStFileUploadPath, searchResult.Files, conf.FileStAuth)
				if err != nil{
					m := fmt.Sprintf("files for %v not posted: %v", result.Id, err)
					logmess.Result.Error = m
					logmess.Result.Status = 3
					logger.Send(&logmess)
					log.Printf("UploadFiles stop working: %v, exiting", m)
					Suicide()
				}
				log.Printf("... files uploaded")
				err = UploadSearchResults(conf.PortalResUploadPath, searchResult, conf.PortalAuth)
				if err != nil{
					m := fmt.Sprintf("results for %v not posted: %v", result.Id, err)
					logmess.Result.Error = m
					logmess.Result.Status = 3
					logger.Send(&logmess)
					log.Printf("UploadSearchResults stop working: %v, exiting", m)
					Suicide()
				}
				urls := []string{searchResult.SearchResult.Uri}
				logmess.EnrichmentTask = &entities.LogEnrichmentTask{
					ProxyCountry: tasklist[result.Id].Region["countryCode"],
					Uri: result.Uri,
				}
				logmess.Result.Array = urls
				logmess.Result.Status = 1
				logger.Send(&logmess)
				log.Printf("... searchResult uploaded")
				postingOk = true
			}
		}
		if result.Proto != nil && conf.ProtoResultConnStr != ""{
			err := UploadFiles(conf.FileStFileUploadPath, result.Files, conf.FileStAuth)
			if err != nil{
				m := fmt.Sprintf("files for %v not posted: %v", result.Id, err)
				logmess.Result.Error = m
				logmess.Result.Status = 3
				logger.Send(&logmess)
				log.Printf("UploadFiles stop working: %v, exiting", m)
				Suicide()
			}
			urls, err := protoP.UploadPsi(&result.Proto)
			if err != nil{
				m := fmt.Sprintf("proto results for %v not posted: %v", result.Id, err)
				logmess.Result.Error = m
				logmess.Result.Status = 3
				logger.Send(&logmess)
				log.Printf("UploadPsi stop working: %v, exiting", m)
				Suicide()
			}
			if result.Urls != nil {
				logmess.SearchTask = &entities.LogSearchTask{
					RuleIds: result.RuleIDs,
					SearchWords: result.SearchWords,
					CountryCode: tasklist[result.Id].Region["countryCode"],
				}
			}
			if result.SearchResults != nil {
				logmess.EnrichmentTask = &entities.LogEnrichmentTask{
					ProxyCountry: tasklist[result.Id].Region["countryCode"],
					Uri: result.Uri,
				}
			}
			logmess.Result.Array = urls
			logmess.Result.Status = 1
			log.Printf("... protohResult uploaded")
			logger.Send(&logmess)
			postingOk = true
		}

		if postingOk && !result.Chunk {
			(*tasklist[result.Id].Task).Ack()
			delete(tasklist,result.Id)
		}
		if !postingOk {
			log.Printf("can't posting %+v", result)
			Suicide()
		}
	}	
}

func Suicide() {
	for _, filename := range []string{"trace.log", "worker-trace.log"}{
		file, err := os.OpenFile(filename, os.O_RDONLY, 0)
		if err != nil{
			continue
		}
		fmt.Printf("=================%v=================\n", filename)
		defer file.Close()
		stat, err := os.Stat(filename)
		if err != nil{
			fmt.Printf("2")
			continue
		}
		if line := stat.Size() - 3072; line > 0{
			buf := make([]byte, 3072)
			file.ReadAt(buf, line)
			fmt.Printf("%s\n", buf)
		}else{
			buf:= make([]byte, stat.Size())
			file.Read(buf)
			fmt.Printf("%s\n", buf)
		}
	}
	panic("stoped")
}

// }