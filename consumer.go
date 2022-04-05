package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"sync"
)

func consume(eChann chan error, i chan []byte, o chan []byte, cmnd string, args ...string){
	cmd := exec.Command(cmnd, args...)
	outpipe, _ := cmd.StdoutPipe()
	inpipe, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		eChann <- fmt.Errorf("can't start \"%v %v\": %v", cmnd, args, err)
	}
	p := cmd.Process.Pid
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func(){
		cmd.Wait()
		eChann <- fmt.Errorf("%v quit unexpectedly", p)
	}()
	go func(){
		reader := bufio.NewReader(outpipe)
		for line, err := reader.ReadString('\n'); err == nil; {
			o <- []byte(line)
			line, err = reader.ReadString('\n')
		}
	}()
	go func(){
		for inp := range(i){
			inp = append(inp, byte('\n'))
			inpipe.Write([]byte(string(inp)))
		}
	}()
	wg.Wait()
	eChann <- fmt.Errorf("%v quit unexpectedly", p)
}
