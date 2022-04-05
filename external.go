package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	// "net/url"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	entities "privatrepo/bp/proto_go_lib"
)
	
	var std_timeout = time.Duration(time.Second)*time.Duration(15)
	
	// Proxy
	func GetProxy(provider string, params map[string]string, reg map[string]string) (string, map[string]string, error){
	req, _ := http.NewRequest("GET", provider, nil)
	req.Header.Add("Accept", "application/json")
	q := req.URL.Query()
	for k,v := range params{
		q.Add(k,v)
	}
	for k,v := range reg{
		q.Add(k,v)
	}
	var last_err error
	req.URL.RawQuery = q.Encode()
	var resp_obj map[string]string
	for i := 1; i <= 10; i++ {
		resp, err := http.DefaultClient.Do(req, )
		if err != nil {
			last_err = err
			continue
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			last_err = err
			continue
		}
		err = json.Unmarshal(body,&resp_obj)
		if err != nil {
			last_err = err
			continue
		}
		return fmt.Sprintf("socks5://%v:%v", resp_obj["ip"], resp_obj["port"]),reg, nil
	}
	return "nok", nil, fmt.Errorf("%v (url: %v)", last_err, req.URL)
}

// Account actions
type AccStor struct{
	s entities.AccountStorageClient
}

type AccHolder struct{
	accs 	*[]string
	counter int32
	ttl		int
	l		*holderStatus
	stor 	*AccStor
	token	string
}
type holderStatus struct{
	allowRead bool
	locker *sync.Cond
}

func (h *AccHolder) Hold(){
	for{
		h.l.locker.L.Lock()
		for !h.l.allowRead{
			h.l.locker.Wait()
		}
		h.l.locker.L.Unlock()
		var i int32
		for i=0; i<h.counter; i++{
			if !h.l.allowRead {
				break
			}
			req := JAccount{AccId: (*h.accs)[i],}
			err := h.stor.ExtendAcc(req, int32(h.ttl), "")
			if err != nil{
				log.Fatalf("error during account extentending: %v", err)
			}
			log.Printf("account %v extended", (*h.accs)[i])
		}
		time.Sleep(time.Duration(time.Second)*time.Duration(int(h.ttl/2)))
	}
}


func (h *AccHolder) Create(numAccs int, ttl int, stor *AccStor, token string){
	arr := make([]string, numAccs)
	h.token = token
	h.accs = &arr
	h.ttl = ttl
	h.stor = stor
	h.l = &holderStatus{true, sync.NewCond(&sync.Mutex{})}
}

func (h *AccHolder) Add(id string){
	h.l.locker.L.Lock()
	h.l.allowRead = false
	h.l.locker.L.Unlock()
	(*h.accs)[h.counter] = id
	atomic.AddInt32(&h.counter, 1)
	h.l.allowRead = true
	h.l.locker.Broadcast()
}

func (h *AccHolder) Del(id string) error {
	h.l.locker.L.Lock()
	h.l.allowRead = false
	h.l.locker.L.Unlock()
	var i int32
	idFounded := false
	for i=0; i<h.counter; i++{
		if (*h.accs)[i] == id{
			idFounded = true
			break
		}
	}
	if !idFounded{
		return fmt.Errorf("cant release account %v, internal error", id)
	}
	(*h.accs) = append((*h.accs)[:i], (*h.accs)[i+1:]...)
	atomic.AddInt32(&h.counter, -1)
	h.l.allowRead = true
	h.l.locker.Broadcast()
	return nil
}


func (s *AccStor)Connect(url string) (error){
	opts := []grpc.DialOption{

		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.Dial(url, opts...)
	if err != nil {
		return err
	}
	s.s = entities.NewAccountStorageClient(conn)
	return nil
}

func (s *AccStor) GetAcc(acctype string, accparams map[string]string, ttl int32, longpool bool) (JAccount, map[string]string, error){
	req := entities.AccountOccupyRequest{}
	if accparams != nil{

		reqparams := make([]*entities.AccountProperty, len(accparams))
		for k,v := range(accparams){
			p := entities.AccountProperty{
				Name: k,
				Value: v,
			}
			reqparams = append(reqparams, &p)	
		}
		req.Properties = reqparams
		req.PropertyRequired = true
	}
	req.LongPool = longpool
	req.AccountType = acctype
	req.Ttl = ttl
	timeoutContext, cancel := context.WithTimeout(context.Background(),  std_timeout)
	defer cancel()
	resp, err := s.s.AccountOccupy(timeoutContext, &req)
	if err != nil{
		return JAccount{}, nil, fmt.Errorf("error during conducting request: %v", err)
	}
	acc := JAccount{}
	if resp.Item.Proxies != nil{
		acc.AccProxy = resp.Item.Proxies[0].Address
	}
	acc.AccId = resp.Id
	acc.Login = resp.Item.Login
	acc.Passwd = resp.Item.Password
	acc.Session = resp.Item.Session
	region := make(map[string]string)
	for _, p := range(resp.Item.Properties){
		if p.Name == "Region"{
			json.Unmarshal([]byte(p.Value), &region)
		}
	}
	return acc, region, nil
}

func (s *AccStor)ExtendAcc(acc JAccount, ttl int32, session string) (error){
	req := entities.AccountExtendRequest{
		AccountId: acc.AccId,
		Login: acc.Login,
		Ttl: ttl,
		Session: acc.Session,
	}
	timeoutContext, cancel := context.WithTimeout(context.Background(),  std_timeout)
	defer cancel()
	resp, err := s.s.AccountExtend(timeoutContext, &req)
	if err != nil{
		return err
	}
	if resp.Status != 200{
		return fmt.Errorf("error during extending account: %v (code %v)", resp.Message, resp.Status)
	}
	return nil
}

func (s *AccStor)BlockAccount(acc JAccount) (error){
	req := entities.AccountDisableRequest{
		Login: acc.Login,
		AccountId: acc.AccId,
	}
	timeoutContext, cancel := context.WithTimeout(context.Background(),  std_timeout)
	defer cancel()
	_, err := s.s.AccountDisable(timeoutContext, &req)
	if err != nil{
		return err
	}
	return nil
}

func (s *AccStor)FreezeAccount(acc JAccount) (error){
	req := entities.AccountExtendRequest{
		AccountId: acc.AccId,
		Login: acc.Login,
		Ttl: 360000,
		Session: acc.Session,
	}
	timeoutContext, cancel := context.WithTimeout(context.Background(),  std_timeout)
	defer cancel()
	resp, err := s.s.AccountExtend(timeoutContext, &req)
	if err != nil{
		return err
	}
	if resp.Status != 200{
		return fmt.Errorf("error during freezing account: %v (code %v)", resp.Message, resp.Status)
	}
	return nil}

func (s *AccStor)LogAccount(record entities.AccountLogRequest) (error){
	timeoutContext, cancel := context.WithTimeout(context.Background(),  std_timeout)
	defer cancel()
	_, err := s.s.AccountLog(timeoutContext, &record)
	if err != nil{
		return err
	}
	return nil
}

// ResultActions via REST

func UploadUrls(uploadpath string, urllist []JSearchResultUrls	, ruleList []string, auth string, e int)(error){
	if len(urllist) == 0{
		return nil
	}
	
	data := JSearchResult{
		Enrichment: e,
		BrandHuntingRuleIds: ruleList,
		Urls: urllist,
	}
	reqdata, err := json.Marshal(data)
	if err != nil{
		return err
	}
	req, _ := http.NewRequest("POST", uploadpath, bytes.NewBuffer(reqdata))
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	if auth != ""{
		req.Header.Add("Authorization", auth)
	}
	postingOK := false
	for t:=0;t<10;t++{
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			// log.Printf("error during posting urls to portal: %v", err)
			return err
		}
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		if resp.Status == "200 OK"{
			postingOK = true
			break
		}
		log.Printf("portal response is %v (Code: %v)", string(body), resp.Status)
		time.Sleep(time.Duration(time.Second)*time.Duration(5+t))
	}
	if !postingOK{
		return fmt.Errorf("url posting failed, data: %s", reqdata)
	}
	return nil
}
	

func UploadSearchResults(uploadpath string, res JEnrichResult, auth string)(error){
	reqdata, err := json.Marshal(res)
	if err != nil{
		return err
	}
	req, _ := http.NewRequest("POST", uploadpath, bytes.NewBuffer(reqdata))
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	if auth != ""{
		req.Header.Add("Authorization", auth)
	}
	postingOK := false
	for t:=0;t<10;t++{
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("error during posting urls to portal: %v", err)
		}
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		if resp.Status == "200 OK"{
			postingOK = true
			break
		}
		log.Printf("portal response is %v (Code: %v)", string(body), resp.Status)
		time.Sleep(time.Duration(time.Second)*time.Duration(5+t))
	}
	if !postingOK{
		return fmt.Errorf("searchresult posting failed, data: %s", reqdata)
	}
	return nil
}

func UploadFiles(uploadpath string, file []*JResultFile, auth string)(error){
	if file == nil {
		return nil
	}
	for _, file := range(file){
		url := fmt.Sprintf("%s/%s?mime=%v", uploadpath, file.Sha256, file.Mimetype)
		reqdata, err := base64.StdEncoding.DecodeString(*file.File)
		if err != nil{
			log.Fatalf("cant decode base64 file\n%v", file.File)
		}
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(reqdata))
		req.Header.Add("Accept", "application/json")
		if auth != ""{
			req.Header.Add("Authorization", auth)
		}
		postingOK := false
		for t:=0;t<10&&!postingOK;t++{
			resp, err := http.DefaultClient.Do(req)
			if err != nil{
				log.Printf("error during posting file to portal: %v", err)
				body, _ := ioutil.ReadAll(resp.Body)
				log.Printf("portal response is %v (Code: %v)", string(body), resp.Status)
			}else{
				postingOK = true
			}
			time.Sleep(time.Duration(time.Second)*time.Duration(5+t))
		}
		if !postingOK{
			return fmt.Errorf("files posting failed, data: %s", reqdata)
		}
		file.File = nil
	}
	return nil
}

// ResultActions via GRPC
type ProtoStor struct{
	p entities.PsiProcessClient
}

func (p *ProtoStor)Connect(protoConnectString string) error{
	opts := []grpc.DialOption{

		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.Dial(protoConnectString, opts...)
	if err != nil {
		return err
	}
	p.p = entities.NewPsiProcessClient(conn)
	return nil

}
func (p *ProtoStor)UploadPsi(data *[]string) ([]string, error){
	if len(*data) == 0{
		return []string{}, nil
	}
	urls := make([]string, len((*data)))
	psiObjectList := make([]*entities.Psi, len(*data))
	for i,s := range(*data){
		var psiObject entities.Psi
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil{
			return nil, err
		}
		err = proto.Unmarshal(b, &psiObject)
		if err != nil{
			return nil, err
		}
		urls[i] = psiObject.PageBrandInfo.Uri
		psiObjectList[i] = &psiObject
	}
	timeOutCtx, cancel := context.WithTimeout(context.Background(),  std_timeout)
	defer cancel()
	req := entities.PsiRequest{
		Data: psiObjectList,
	}
	_, err :=p.p.Pour(timeOutCtx, &req)
	if err != nil{
		return nil, err
	}
	return urls, nil
}


type LogEndp struct {
	c entities.LogPostClient
	sname string
	connected bool
}

func (s *LogEndp) Connect(protoConnectString string, sname string) error{
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.Dial(protoConnectString, opts...)
	if err != nil {
		return err
	}
	s.connected = true
	s.c = entities.NewLogPostClient(conn)
	s.sname = sname
	return nil
}

func (s *LogEndp) Send(m *entities.Log){
	if !s.connected {
		return
	}
	tmctx, cancel := context.WithTimeout(context.Background(), std_timeout)
	defer cancel()
	m.Source = s.sname
	s.c.Do(tmctx, m)
}
