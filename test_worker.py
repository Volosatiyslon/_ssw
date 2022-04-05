import os
import asyncio
import concurrent
import sys
import json
import logging
import time
from random import randint, randrange
from unittest import result

logging.basicConfig(level=logging.INFO, filename="test_worker.log")
reader = None
writer = None
count = 3

async def stdio(limit=asyncio.streams._DEFAULT_LIMIT, l=None):
    if l is None:
        l = asyncio.get_event_loop()
    r_r = asyncio.StreamReader(limit=limit, loop=l)
    await l.connect_read_pipe(lambda: asyncio.StreamReaderProtocol(r_r, loop=l), sys.stdin)
    writer_transport, writer_protocol = await l.connect_write_pipe(lambda: asyncio.streams.FlowControlMixin(loop=l), os.fdopen(sys.stdout.fileno(), 'wb'))
    w_r = asyncio.streams.StreamWriter(writer_transport, writer_protocol, None, l)
    globals()['reader'] = r_r
    globals()['writer'] = w_r

async def read(q: asyncio.Queue) -> None:
    await stdio()
    global reader
    logging.info("READ started")
    while True:
        # print(dir(reader))
        # sys.exit()
        raw_task = await reader.readuntil("\n".encode())
        logging.info(f"READ got \"{raw_task}\"")
        try:
            task = json.loads(raw_task)
        except Exception:
            logging.error("cant parse task")
            continue
        task["starttime"]=time.monotonic()
        await q.put(task)
        logging.info(f"READ put task to worker")


async def worker(input_q: asyncio.Queue, output_q: asyncio.Queue, f: str) -> None:
    logging.info(f"WORKER started")
    while True:
        task = await input_q.get()
        logging.info(f"WORKER got \"{task}\"")
        await asyncio.sleep(5.0)
        # result = task
        with open("out.json") as f:
            result = json.loads(f.read())
        result["id"] = task.get("id")
        # target_field = task.get("searchWords")
        # if target_field:
        #     await asyncio.sleep(randint(50, 400)/1000)
        #     task["urls"] = []
        #     for _ in range(3,randint(4,7)):
        #         task["urls"].append({"title":eval(f)(task["searchWords"]), "url":"aliexpress.com"})
        #     await asyncio.sleep(randint(0,3))
        # else:
        #     task["error"] = "%target undefined%"
        # start_time =  task.pop("starttime")
        # task["time"] = int((time.monotonic()-start_time)*1000)
        logging.info(f"WORKER sent answer")
        await output_q.put(result)


async def writer(output_q: asyncio.Queue) -> None:
    logging.info(f"WRITER started")
    global writer
    while True:
        logging.info(f"WRITER got answer")
        result = await output_q.get()
        raw_result = "UNEXPECTEDOUTPUT!!!"
        try:
            raw_result = json.dumps(result)
        except Exception:
            pass
        writer.write(f'{raw_result}\n'.encode())    
        logging.info(f"WRITER sent asnwer")
            
async def suicide():
    await asyncio.sleep(7)
    sys.exit()

if __name__ == "__main__":
    try:
        filename = sys.argv[1]
        with open(filename, "rb") as f:
            template = json.loads(f.read().decode())
    except:
        logging.info("cant read conf file")
        sys.exit()
    l = template["lambda"]
    loop = asyncio.get_event_loop()
    input_queue = asyncio.Queue()
    output_queue = asyncio.Queue()
    tasks = [read(input_queue), writer(output_queue), suicide()]
    for _ in range(0, count):
        tasks.append(worker(input_queue, output_queue, l))
    try:
        done, _ = loop.run_until_complete(asyncio.wait(tasks, return_when=concurrent.futures.FIRST_EXCEPTION))
        for c in done:
            logging.error(c.exception)
            c.print_stack()
    except KeyboardInterrupt:
        writer.write("exiting\n".encode())
        sys.exit()
