package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/streadway/amqp"
	immortalampq "provatrepo/libs/rabbitImmortal"

	tsclient "provatrepo/bp/taskscheduler-go-client"
	"golang.org/x/net/websocket"
)
type tsTask struct{
	body string
	id string
	ws *wsTaskProvider
}

type rmqTask struct{
	t	*amqp.Delivery
	id	string
}

type wsTaskProvider struct {
	taskQueue chan tsTask
	limiter chan int
	client *tsclient.Client
	reqId uint32
}

type searchTask struct {
	ID string `json:"id"`
}

type Task interface{
	Ack()
	Nack()
	GetBody() 	string
	GetId()		string
}
func (t *tsTask) GetBody() string{
	return t.body
}

func (t *tsTask) Ack(){
	atomic.AddUint32(&t.ws.reqId, 1)
	reqId := strconv.Itoa(int(t.ws.reqId))
	taskId := t.id
	req := tsclient.WSInput{
		Id: reqId,
		Method: "ack",
		Params: map[string]string{
			"id":taskId,
		},
	}
	var resp *tsclient.WSResponse
	for resp == nil{
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(uint64(time.Second)*uint64(30)))
		defer cancel()
		resp, err := t.ws.client.Send(&req,ctx)
		if err==nil && resp.Error == ""{
			<- t.ws.limiter
			break
		}else{
			if resp.Error != ""{
				log.Printf("ws resp error: %v", resp.Error)
			}
			if err != nil{
				log.Printf("wsclient error: %v", err)
			}
			resp = nil
			time.Sleep(time.Second)
		}
	}
}

func (t *tsTask) Nack(){
	atomic.AddUint32(&t.ws.reqId, 1)
	reqId := strconv.Itoa(int(t.ws.reqId))
	taskId := t.id
	req := tsclient.WSInput{
		Id: reqId,
		Method: "nack",
		Params: map[string]string{
			"id":taskId,
		},
	}
	var resp *tsclient.WSResponse
	for resp == nil{
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(uint64(time.Second)*uint64(30)))
		defer cancel()
		resp, err := t.ws.client.Send(&req,ctx)
		if err==nil && resp.Error == ""{
			<- t.ws.limiter
			break
		}else{
			if resp.Error != ""{
				log.Printf("ws resp error: %v", resp.Error)
			}
			if err != nil{
				log.Printf("wsclient error: %v", err)
			}
			resp = nil
			time.Sleep(time.Second)
		}
	}
	log.Printf("ack resp: %+v", resp)
}

func (t *tsTask) GetId() string{
	return t.id
}

func (r *rmqTask) Ack(){
	r.t.Ack(false)
}

func (r *rmqTask) Nack(){
	r.t.Nack(false, true)	
}

func (r *rmqTask) GetBody() string{
	return string(r.t.Body)
}

func (r *rmqTask) GetId() string{
	return r.id
}

func NewWSTaskProvider(u string, pCount uint16) (c *chan Task , err error){
	connection, err := websocket.Dial(u, "","http://bp-search-service-wrapper")
	if err != nil{
		return nil, err
	}
	wsclient, eventChann := tsclient.NewClient(connection, time.Duration(int64(30)*int64(time.Second)))
	// wsclient, _ := tsclient.NewClient(connection, time.Duration(int64(30)*int64(time.Second)))
	// client, _ := tsclient.NewClient(connection, time.Duration(int64(30)*int64(time.Second)))
	go wsclient.Run()
	go func(){
		log.Print("listner started")
		for _ = range eventChann{
			continue
		}
	}()
	provider := &wsTaskProvider{
		taskQueue: make(chan tsTask),
		limiter: make(chan int, pCount),
		client: wsclient,
	}
	reschan := make (chan Task)
	go func(){
			for{
				provider.limiter <- 1
				log.Print("asking a task")
				atomic.AddUint32(&provider.reqId, 1)
				reqId := strconv.Itoa(int(provider.reqId))
				var resp *tsclient.WSResponse
				req := tsclient.WSInput{
					Id: reqId,
					Method: "get",
				}
				for resp == nil{
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(uint64(time.Second)*uint64(30)))
					resp, _ = provider.client.Send(&req, ctx)
					if resp.Error != ""{
						time.Sleep(time.Second)
						log.Printf("WS response: %v", err)
						resp = nil
						cancel()
					}
					cancel()
				}
				var st searchTask
				json.Unmarshal(resp.Result, &st)
				reschan <- &tsTask{
					body: string(resp.WSOutput.Result),
					id: st.ID,
					ws: provider,
				}
			}
		}()
		return &reschan, nil
	}

func NewTaskProvider(u string, qname string, pCount uint16) (c *chan Task , err error){
	if qname != ""{
		conn, err := immortalampq.Dial(u, func (e error) bool {log.Print(e);return true}, func(){})
		if err != nil{
			return nil, err
		}
		rmqchan, err := conn.Channel(func (e error) bool {log.Print(e);return true}, func(){})
		if err != nil{
			return nil, err
		}
		resultch := make(chan Task)
		err = rmqchan.Qos(int(pCount), 0, false)
		if err != nil{
			return nil, err
		}
		ch, err := rmqchan.Consume(qname, fmt.Sprintf("%v-reader", qname), false, false, false, false, nil, func (e error) bool {log.Print(e);return true})
		go func(){
			cur_id := rand.Uint32()
			for m := range(ch){
				m_id := strconv.Itoa(int(cur_id))
				atomic.AddUint32(&cur_id, 1)
				resultch <- &rmqTask{
					id: m_id,
					t: &m,
				}
			}
		}()
		return &resultch, nil
	}else{
		return NewWSTaskProvider(u, pCount)
	}
}



// func main(){
// 	mchann, err := NewTaskProvider("ws://taskscheduler/listener?app=joomcom", "", 1)
// 	if err != nil{
// 		log.Fatalf("%v", err)
// 	}
// 	i := 0
// 	for m := range *mchann{
// 		log.Printf("recived task: %+v", m)
// 		i++
// 		if i == 1{
// 			log.Printf("acking %v", m.GetBody())
// 			m.Ack()
// 			log.Printf("acked")

// 		}
// 	}
// }


