package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/DSiSc/p2p/tools/common"
	"github.com/DSiSc/p2p/tools/statistics/client"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const syncerInterval = 60

func main() {
	var statisticsServer string
	var nodeCount, blockInterval int
	flagSet := flag.NewFlagSet("syncer-test", flag.ExitOnError)
	flagSet.StringVar(&statisticsServer, "server", "localhost:8080", "statistics server address, default localhost:8080")
	flagSet.IntVar(&nodeCount, "nodes", 1, "p2p node count")
	flagSet.IntVar(&blockInterval, "interval", 2, "block produce interval")
	flagSet.Usage = func() {
		fmt.Println(`Justitia blockchain syncer p2p test tool.
Usage:
	syncer-test [-server localhost:8080 -nodes 1 -interval 2]

Examples:
	syncer-test -server localhost:8080  -nodes 1 -interval 2`)
		fmt.Println("Flags:")
		flagSet.PrintDefaults()
	}
	flagSet.Parse(os.Args[1:])

	statisticsClient := client.NewStatisticsClient(statisticsServer)

	topo, err := statisticsClient.GetTopos()
	if err != nil {
		fmt.Printf("Failed to get topo info from server, as: %v", err)
		os.Exit(1)
	}
	reachability := client.TopoReachbility(topo)
	if reachability < nodeCount {
		fmt.Printf("The net reachability is %d, less than nodes count： %d\n", reachability, nodeCount)
		os.Exit(1)
	}

	// syncer interval is 60 sec
	if heightStatistic(topo, syncerInterval/blockInterval) {
		os.Exit(0)
	} else {
		os.Exit(2)
	}
}

// calculate peer's height and the deviation with there neighbors
func heightStatistic(topo map[string][]*common.Neighbor, standardDeviation int) bool {
	for peer, neighbors := range topo {
		pH := getPeerHeight(peer)
		for _, neighbor := range neighbors {
			nH := getPeerHeight(neighbor.Address)
			if stdDeviation(nH, pH) > uint64(standardDeviation) {
				return false
			}
		}
	}
	return true
}

// calculate the deviation of the two value
func stdDeviation(v1, v2 uint64) uint64 {
	if v1 > v2 {
		return v1 - v2
	} else {
		return v2 - v1
	}
}

// get peer's height
func getPeerHeight(peer string) uint64 {
	addr := peer[:strings.Index(peer, ":")]
	resp, err := http.Get("http://" + addr + ":" + strconv.Itoa(47768) + "/eth_blockNumber")
	if err != nil {
		fmt.Printf("Failed to get p2p topo info, as: %v\n", err)
		return 0
	}
	var result map[string]string
	parseResp(&result, resp)
	height, _ := strconv.ParseUint(result["result"][2:], 16, 64)
	resp.Body.Close()
	fmt.Printf("%s height: %d\n", addr, height)
	return height
}

// parse server response
func parseResp(v interface{}, resp *http.Response) {
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Failed to read response body, as: %v\n", err)
	}
	err = json.Unmarshal(body, v)
	if err != nil {
		fmt.Printf("Failed to parse response body, as: %v\n", err)
	}

}
