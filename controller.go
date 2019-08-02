package main

import (
	"fmt"
	"net"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-iptables/iptables"
	"github.com/golang/glog"
	"github.com/pierrec/xxHash/xxHash32"
)

// ContentHashSeed is the seed used for hashing the iptable rules.
const ContentHashSeed = 0xDEAD

// NATTable represents the nat-table in iptables
const NATTable = "nat"

// FilterTable represents the filter-table in iptables
const FilterTable = "filter"

// Controller is a controller which monitors iptables and loadbalancers and updates iptables accordingly.
type Controller struct {
	sync.Mutex
	loadbalancers    map[string]Loadbalancer
	started          bool
	stopCh           chan struct{}
	ipt              *iptables.IPTables
	mainChainName    string
	forwardChainName string
	tickRate         int
	metrics          *Metrics
}

// NewController creates a new Controller instance.
func NewController(tickRate int, metrics *Metrics) (*Controller, error) {
	ipt, err := iptables.New()
	if err != nil {
		return nil, fmt.Errorf("couldn't init iptables, see: %v", err)
	}

	return &Controller{
		loadbalancers:    make(map[string]Loadbalancer),
		ipt:              ipt,
		stopCh:           make(chan struct{}),
		mainChainName:    "iptableslb-prerouting",
		forwardChainName: "iptableslb-forward",
		tickRate:         tickRate,
		metrics:          metrics,
	}, nil
}

// UpsertLoadbalancer inserts or updates the passed loadbalancer in the controller.
func (c *Controller) UpsertLoadbalancer(lb *Loadbalancer) {
	c.Lock()
	defer c.Unlock()

	if len(lb.Outputs) == 0 {
		// empty loadbalancer? kill it!
		delete(c.loadbalancers, lb.Key())
		return
	}

	lbCopy := *lb
	lbCopy.MarkUpdated()

	c.loadbalancers[lb.Key()] = lbCopy
}

// DeleteLoadbalancer removes the passed loadbalancer from the controller.
func (c *Controller) DeleteLoadbalancer(lb *Loadbalancer) {
	c.Lock()
	defer c.Unlock()

	delete(c.loadbalancers, lb.Key())
}

func (c *Controller) countError() {
	if c.metrics != nil {
		c.metrics.ErrorsTotal.Inc()
	}
}

// Stop stops the controller
func (c *Controller) Stop() {
	close(c.stopCh)

	// Block till everything is down
	for c.started {
		time.Sleep(1 * time.Second)
	}
}

// Run starts the controller main loop. Calling it doesn't block!
func (c *Controller) Run() {
	if c.started {
		return
	}

	c.started = true

	go (func() {
		glog.Infof("Controller started.")

		mainLoopStopCh := c.loop("MainLoop", time.Duration(c.tickRate)*time.Second, c.sync)

		<-c.stopCh

		close(mainLoopStopCh)
		c.started = false

		glog.Infof("Controller stopped.")
	})()
}

func (c *Controller) loop(name string, waitTime time.Duration, cb func()) chan struct{} {
	stopCh := make(chan struct{})
	timer := time.NewTimer(waitTime)

	go (func() {
		for {
			select {
			case <-timer.C:
				startTime := time.Now()
				glog.V(4).Infof("started syncing %s", name)

				cb()

				neededTime := time.Since(startTime)
				glog.V(4).Infof("finished syncing %s in %s", name, neededTime.String())

				timer.Reset(waitTime)

			case <-stopCh:
				if !timer.Stop() {
					<-timer.C // discard content
				}

				return
			}
		}
	})()

	return stopCh
}

// Task represents a task which should be executed in an isolated environment (as in: always fresh args, no side-effects)
type Task func(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID)

func (c *Controller) sync() {
	c.Lock()
	defer c.Unlock()

	tasks := []Task{
		c.deleteChainsStuckInCreation,
		c.refreshLoadbalancersWithBrokenChains,
		c.ensureForwardChainExists,
		c.ensureForwardChainEntries,
		c.ensureMainChainExists,
		c.ensureChains,
		c.ensureMainChainEntries,
		c.deleteObsoleteMainChainEntries,
		c.deleteObsoleteChains,
		c.deleteObsoleteForwardChainEntries,
	}

	// Always get data from iptables to avoid running into mismatches between our state and iptables state
	for _, t := range tasks {
		taskName := runtime.FuncForPC(reflect.ValueOf(t).Pointer()).Name()

		glog.V(5).Infof("starting %s", taskName)

		allChains, err := c.ipt.ListChains(NATTable)
		if err != nil {
			c.countError()
			glog.Errorf("couldn't list all chains in nat table, see: %v", err)
			continue
		}

		chainIDs := c.findChainIDs(allChains)
		lbToChains := c.mapLoadbalancerKeyToChainIDs(chainIDs)

		t(allChains, chainIDs, lbToChains)

		glog.V(5).Infof("finished %s", taskName)
	}

	if c.metrics != nil {
		c.updateLBMetrics()
	}
}

func (c *Controller) updateLBMetrics() {
	c.metrics.LBHealthy.Set(float64(len(c.loadbalancers)))

	for key, lb := range c.loadbalancers {
		c.metrics.LBHealthyEndpoints.WithLabelValues(key).Set(float64(len(lb.Outputs)))
	}
}

func (c *Controller) refreshLoadbalancersWithBrokenChains(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	// Check all loadbalancer chains, calculate hash, compare with chainname
	// IF mismatch: Set lbs LastUpdate to now, so it will be recreated on the next cycle

	for lbKey, chains := range lbToChains {
		lb, found := c.loadbalancers[lbKey]
		if !found {
			glog.V(5).Infof("skipping validating content hashes for chains from lb `%s` since it's deleted anyways", lbKey)
			continue
		}

		for _, chain := range chains {
			rules, err := c.ipt.List(NATTable, chain.String())
			if err != nil {
				glog.Errorf("couldn't retrieve rules in chain `%s`, see: %v", chain.String(), err)
				c.countError()
				continue
			}

			hash := c.calculateHashForRules(rules)

			if hash != chain.ContentHash {
				glog.Warningf("chain `%s` for lb `%s` got manipulated, content hash isn't matching anymore, marking lb as updated so it gets recreated.", chain.String(), lbKey)
				lb.MarkUpdated()
				c.loadbalancers[lbKey] = lb
			}
		}
	}
}

func (c *Controller) ensureChains(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	// For every loadbalancer, check if a corresponding chain exists, if not, create
	for lbKey, chains := range lbToChains {
		lb, found := c.loadbalancers[lbKey]
		if !found {
			glog.V(4).Infof("skipping ensuring chains for lb `%s` since it's in iptables but not our configuration.", lbKey)
			continue
		}

		existingChainName := ""
		for _, chain := range chains {
			if chain.State == ChainCreated && chain.LastUpdate == lb.LastUpdate {
				existingChainName = chain.String()
			}
		}

		if existingChainName != "" {
			glog.V(5).Infof("skipping ensure chain for lb `%s` since chain `%s` already exists", lbKey, existingChainName)
			continue
		}

		_, err := c.createChainForLB(&lb)
		if err != nil {
			glog.Errorf("couldn't create chain for lb `%s`, see: %v", lbKey, err)
			c.countError()
		}
	}
}

func (c *Controller) getChainIDsWithState(chainIDs []ChainID, state ChainState) []ChainID {
	filteredChainIDs := make([]ChainID, 0)

	for _, chainID := range chainIDs {
		if chainID.State == state {
			filteredChainIDs = append(filteredChainIDs, chainID)
		}
	}

	return filteredChainIDs
}

func (c *Controller) getLatestChainID(chainIDs []ChainID) ChainID {
	var latest ChainID

	for _, chainID := range chainIDs {
		if chainID.LastUpdate > latest.LastUpdate {
			latest = chainID
		}
	}

	return latest
}

func (c *Controller) ensureMainChainEntries(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	// For every chain, check if a corresponding entry in the main chain exists, if not, create
	rules, err := c.ipt.List(NATTable, c.mainChainName)
	if err != nil {
		glog.Errorf("couldn't retrieve rules in mainChain `%s`, see: %v", c.mainChainName, err)
		return
	}

	for lbKey, chains := range lbToChains {
		_, found := c.loadbalancers[lbKey]
		if !found {
			glog.V(4).Infof("skipping ensuring mainChain entries for lb `%s` since it's in iptables but not our configuration.", lbKey)
			continue
		}

		createdChains := c.getChainIDsWithState(chains, ChainCreated)
		if len(createdChains) == 0 {
			glog.V(4).Infof("skipping mainChainEntries for lb `%s` since no chains have been created for it yet", lbKey)
			continue
		}

		latest := c.getLatestChainID(createdChains)
		rule := c.getRuleStringForMainChainEntryToChain(latest)

		if c.rulesContainRule(rules, rule) {
			glog.V(5).Infof("skipping mainChainEntries for lb `%s` since newest chain `%s` already exists", lbKey, latest.String())
			continue
		}

		err = c.ipt.Append(NATTable, c.mainChainName, strings.Split(rule, " ")...)
		if err != nil {
			glog.Errorf("couldn't create mainChain entry for lb `%s` to chain `%s`, see: %v", lbKey, latest.String(), err)
			c.countError()
			continue
		}

		glog.Infof("added mainChain entry for lb `%s` to chain `%s`", lbKey, latest.String())
	}
}

func (c *Controller) rulesContainRule(rules []string, rule string) bool {
	splittedRule := strings.Split(rule, " ")
	tuples := make([]string, 0)

	for i := 0; i < len(splittedRule); i++ {
		if i%2 == 1 {
			tuples = append(tuples, splittedRule[i-1]+" "+splittedRule[i])
		}
	}

	for _, r := range rules {
		if c.allStringsInString(tuples, r) {
			return true
		}
	}

	return false
}

func (c *Controller) allStringsInString(all []string, str string) bool {
	for _, s := range all {
		if strings.Index(str, s) < 0 {
			return false
		}
	}

	return true
}

func (c *Controller) deleteObsoleteChains(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	// Remove all chains which ain't referenced in mainchain
	rules, err := c.ipt.List(NATTable, c.mainChainName)
	if err != nil {
		glog.Errorf("couldn't retrieve rules in mainChain `%s`, see: %v", c.mainChainName, err)
		c.countError()
		return
	}

	referencedChains := make([]ChainID, 0)
	for _, rule := range rules {
		if rule == "-N "+c.mainChainName {
			continue
		}

		chainID, err := c.getChainIDForMainChainRule(rule)
		if err != nil {
			glog.Errorf("couldn't get chainid for mainchain rule `%s`, see: %v", rule, err)
			c.countError()
			continue
		}

		referencedChains = append(referencedChains, chainID)
	}

	for _, chainID := range chainIDs {
		if !c.chainIDsContainID(referencedChains, chainID) {
			err = c.deleteChain(chainID)
			if err != nil {
				glog.Errorf("couldn't delete obsolete chain `%s` for lb `%s`, see: %v", chainID.String(), chainID.AsLoadbalancerKey(), err)
				c.countError()
				continue
			}

			glog.Infof("Removed chain `%s` for deleted lb `%s`", chainID.String(), chainID.AsLoadbalancerKey())
		}
	}
}

func (c *Controller) chainIDsContainID(ids []ChainID, id ChainID) bool {
	for _, i := range ids {
		if i.String() == id.String() {
			return true
		}
	}

	return false
}

func (c *Controller) deleteObsoleteMainChainEntries(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	// Map loadbalancer to chain, delete all rules except the latest
	// in case lb isn't in config at all, remove it
	rules, err := c.ipt.List(NATTable, c.mainChainName)
	if err != nil {
		glog.Errorf("couldn't retrieve rules in mainChain `%s`, see: %v", c.mainChainName, err)
		c.countError()
		return
	}

	lbToChains = make(map[string][]ChainID)

	for _, rule := range rules {
		if rule == "-N "+c.mainChainName {
			continue
		}

		chainID, err := c.getChainIDForMainChainRule(rule)
		if err != nil {
			glog.Errorf("couldn't get chainid for mainchain rule `%s`, see: %v", rule, err)
			c.countError()
			continue
		}

		key := chainID.AsLoadbalancerKey()
		lbToChains[key] = append(lbToChains[key], chainID)
	}

	for lbKey, chains := range lbToChains {
		// LB got deleted from config, but is still in iptables -> delete it from iptables
		if _, exists := c.loadbalancers[lbKey]; !exists {
			for _, chain := range chains {
				err = c.removeMainChainEntryToChain(chain)
				if err != nil {
					glog.Errorf("couldn't remove main chain entry referencing chain `%s` for deleted lb `%s`, see: %v", chain.String(), lbKey, err)
					c.countError()
					continue
				}

				glog.Infof("Removed main chain entry referencing chain `%s` for deleted lb `%s`", chain.String(), lbKey)
			}

			continue
		}

		// If there's only one chain reference, we dont have to delete shit
		if len(chains) == 1 {
			continue
		}

		// Multiple chains? Let's see which one is the newest and kick out all others
		newestChain := chains[0]
		for _, chain := range chains {
			if chain.LastUpdate > newestChain.LastUpdate {
				newestChain = chain
			}
		}

		for _, chain := range chains {
			if chain.String() != newestChain.String() {
				err = c.removeMainChainEntryToChain(chain)
				if err != nil {
					glog.Errorf("couldn't remove outdated main chain entry referencing chain `%s` for lb `%s`, see: %v", chain.String(), lbKey, err)
					c.countError()
					continue
				}

				glog.Infof("Removed outdated main chain entry referencing chain `%s` for lb `%s`", chain.String(), lbKey)
			}
		}
	}
}

func (c *Controller) getChainIDForMainChainRule(rule string) (ChainID, error) {
	start := strings.Index(rule, " -j")
	if start < 0 {
		return ChainID{}, fmt.Errorf("couldn't find jump target in rule `%s`", rule)
	}

	offs := 4
	if len(rule) <= start+offs {
		return ChainID{}, fmt.Errorf("whooops, we failed hard trying to parse the rules in the main chain, couldnt parse `%s`", rule)
	}

	substr := rule[start+offs:]
	ws := strings.Index(substr, " ")
	if ws < 0 {
		ws = len(substr)
	}

	substr = substr[:ws]

	return TryParseChainID(substr)
}

func (c *Controller) getDestinationFromRule(rule string) (Endpoint, error) {
	args := strings.Split(rule, " ")

	for i := 0; i < len(args); i++ {
		if args[i] == "--to-destination" && len(args) > i+1 {
			endpoint, err := TryParseEndpoint(args[i+1])
			if err != nil {
				return Endpoint{}, fmt.Errorf("couldn't parse endpoint from --to-destination arg, see: %v", err)
			}

			return endpoint, nil
		}
	}

	return Endpoint{}, fmt.Errorf("couldn't find --to-destination arg in rule `%s`", rule)
}

func (c *Controller) getDestinationFromForwardRule(rule string) (Endpoint, error) {
	args := strings.Split(rule, " ")

	sIP := ""
	dIP := ""
	sPort := 0
	dPort := 0
	var err error

	for i := 0; i < len(args); i++ {
		if len(args) <= i+1 {
			break
		}

		nextArg := args[i+1]

		if args[i] == "-s" {
			sIP = nextArg
		}

		if args[i] == "-d" {
			dIP = nextArg
		}

		if args[i] == "--sport" {
			sPort, err = strconv.Atoi(nextArg)
			if err != nil {
				return Endpoint{}, fmt.Errorf("couldn't atoi sPort in rule `%s`, see: %v", rule, err)
			}
		}

		if args[i] == "--dport" {
			dPort, err = strconv.Atoi(nextArg)
			if err != nil {
				return Endpoint{}, fmt.Errorf("couldn't atoi dPort in rule `%s`, see: %v", rule, err)
			}
		}
	}

	getEndpoint := func(ip string, port int) Endpoint {
		if idx := strings.LastIndex(ip, "/"); idx >= 0 {
			ip = ip[:idx]
		}

		return Endpoint{IP: net.ParseIP(ip).To4(), Port: uint16(port)}
	}

	if sIP != "" && dIP != "" {
		return Endpoint{}, fmt.Errorf("broken rule `%s` got source and dest ip", rule)
	} else if sPort != 0 && dPort != 0 {
		return Endpoint{}, fmt.Errorf("broken rule `%s` got source and dest port", rule)
	} else if sIP != "" && sPort != 0 {
		return getEndpoint(sIP, sPort), nil
	} else if dIP != "" && dPort != 0 {
		return getEndpoint(dIP, dPort), nil
	}

	return Endpoint{}, fmt.Errorf("unknown rule `%s` doesnt have source ip + source port or dest ip + dest port", rule)
}

func (c *Controller) stripNARules(rule string) string {
	newRule := ""
	rules := strings.Split(rule, " ")
	skipArg := false

	for _, rule := range rules {
		if rule == "-A" || rule == "-N" {
			skipArg = true
			continue
		}

		if skipArg {
			skipArg = false
			continue
		}

		if newRule != "" {
			newRule = newRule + " " + rule
		} else {
			newRule = rule
		}
	}

	return newRule
}

func (c *Controller) calculateHashForRules(rules []string) uint32 {
	x := xxHash32.New(ContentHashSeed)

	for _, rule := range rules {
		// So, since -A and -N contain the chain name and the chainname contains the hash we'll simply skip these
		x.Write([]byte(c.stripNARules(rule)))
	}

	return x.Sum32()
}

func (c *Controller) createChainForLB(lb *Loadbalancer) (ChainID, error) {
	lenOutputs := len(lb.Outputs)
	if lenOutputs == 0 {
		return ChainID{}, fmt.Errorf("zero outputs defined for lb `%s`, dunno what to do here, not creating chain", lb.Key())
	}

	chain := lb.GetChainID(ChainCreating, 0)
	err := c.ipt.NewChain(NATTable, chain.String())
	if err != nil {
		return ChainID{}, fmt.Errorf("couldn't create chain `%s` for lb `%s`, see: %v", chain.String(), lb.Key(), err)
	}

	glog.Infof("created chain `%s` for lb `%s`", chain.String(), lb.Key())
	rules := make([]string, 0)

	// Outputs 3 - 1 need statistic magic to match only every nth conn
	for i := lenOutputs; i > 1; i-- {
		output := lb.Outputs[i-1]

		rule := fmt.Sprintf("-p %s -d %s --dport %d -m statistic --mode nth --every %d --packet 0 -j DNAT --to-destination %s", lb.Protocol.String(), lb.Input.IP.String(), lb.Input.Port, i, output.String())
		err = c.ipt.Append(NATTable, chain.String(), strings.Split(rule, " ")...)
		if err != nil {
			return ChainID{}, fmt.Errorf("couldn't create rule `%s` in chain `%s` for output `%s` lb `%s`, see: %v", rule, chain.String(), output.String(), lb.Key(), err)
		}

		rules = append(rules, rule)
	}

	// Final output always matches everything not matched yet.
	rule := fmt.Sprintf("-p %s -d %s --dport %d -j DNAT --to-destination %s", lb.Protocol.String(), lb.Input.IP.String(), lb.Input.Port, lb.Outputs[0].String())
	err = c.ipt.Append(NATTable, chain.String(), strings.Split(rule, " ")...)
	if err != nil {
		return ChainID{}, fmt.Errorf("couldn't create rule `%s` in chain `%s` for output `%s` lb `%s`, see: %v", rule, chain.String(), lb.Outputs[0].String(), lb.Key(), err)
	}

	rules = append(rules, rule)

	// Get rules from remote for hashing, since iptables adds some kungfu, changes arg order, etc.
	rules, err = c.ipt.List(NATTable, chain.String())
	if err != nil {
		return ChainID{}, fmt.Errorf("couldn't retrieve rules in chain `%s`, see: %v", chain.String(), err)

	}

	newChainID := lb.GetChainID(ChainCreated, c.calculateHashForRules(rules))

	err = c.ipt.RenameChain(NATTable, chain.String(), newChainID.String())
	if err != nil {
		return ChainID{}, fmt.Errorf("couldn't rename chain `%s` (creating) to `%s` (created) for lb `%s`, see: %v", chain.String(), newChainID.String(), lb.Key(), err)
	}

	return newChainID, nil
}

func (c *Controller) getRuleStringForMainChainEntryToChain(chain ChainID) string {
	return fmt.Sprintf("-p %s -d %s --dport %d -j %s", chain.Protocol.String(), chain.IP.String(), chain.Port, chain.String())
}

func (c *Controller) removeMainChainEntryToChain(chain ChainID) error {
	rule := c.getRuleStringForMainChainEntryToChain(chain)
	err := c.ipt.Delete(NATTable, c.mainChainName, strings.Split(rule, " ")...)
	if err != nil {
		// FIXME:  ignore "rule not exists" errors
		return fmt.Errorf("couldn't remove rule `%s` for lb `%s` from main chain, see: %v", rule, chain.AsLoadbalancerKey(), err)
	}

	return nil
}

func (c *Controller) mapLoadbalancerKeyToChainIDs(chainIDs []ChainID) map[string][]ChainID {
	lbToChain := make(map[string][]ChainID)

	// Gather lbs in iptables
	for _, chainID := range chainIDs {
		key := chainID.AsLoadbalancerKey()
		lbToChain[key] = append(lbToChain[key], chainID)
	}

	// Gather lbs in config
	for _, lb := range c.loadbalancers {
		key := lb.Key()

		if _, existing := lbToChain[key]; !existing {
			lbToChain[key] = make([]ChainID, 0)
		}
	}

	return lbToChain
}

func (c *Controller) findChainIDs(chains []string) []ChainID {
	chainIDs := make([]ChainID, 0)

	for _, chain := range chains {
		chainID, err := TryParseChainID(chain)
		if err != nil {
			glog.V(6).Infof("skipping chain `%s` since it's not a valid ChainID, see: %v", chain, err)
			continue
		}

		chainIDs = append(chainIDs, chainID)
	}

	return chainIDs
}

func (c *Controller) deleteChain(chainID ChainID) error {
	chainName := chainID.String()

	err := c.ipt.ClearChain(NATTable, chainName)
	if err != nil {
		return fmt.Errorf("couldn't flush chain `%s` (%s), see: %v", chainName, chainID.AsLoadbalancerKey(), err)
	}

	err = c.ipt.DeleteChain(NATTable, chainName)
	if err != nil {
		return fmt.Errorf("couldn't delete chain `%s` (%s), see: %v", chainName, chainID.AsLoadbalancerKey(), err)
	}

	return nil
}

func (c *Controller) deleteChainsStuckInCreation(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	for _, chainID := range chainIDs {
		if chainID.State == ChainCreating {
			glog.Warningf("chain `%s` (%s) stuck in creation, deleting it...", chainID.String(), chainID.AsLoadbalancerKey())

			err := c.deleteChain(chainID)
			if err != nil {
				glog.Errorf("couldn't cleanup chain stuck in creation, see: %v", err)
				c.countError()
			}
		}
	}
}

func (c *Controller) ensureMainChainExists(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	found := false
	for _, chain := range allChains {
		if chain == c.mainChainName {
			found = true
			glog.V(4).Infof("skipping creation of mainchain since it already exists")
			return
		}
	}

	if !found {
		glog.V(4).Infof("creating mainchain...")
		err := c.ipt.NewChain(NATTable, c.mainChainName)
		if err != nil {
			glog.Errorf("couldn't create mainchain, see: %v", err)
			c.countError()
			return
		}
		glog.V(4).Infof("created mainchain")
	}
}

func (c *Controller) ensureForwardChainExists(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	allChains, err := c.ipt.ListChains(FilterTable)
	if err != nil {
		glog.Errorf("couldn't list all chains in filter table, see: %v", err)
		c.countError()
		return
	}

	found := false
	for _, chain := range allChains {
		if chain == c.forwardChainName {
			found = true
			glog.V(4).Infof("skipping creation of forward chain since it already exists")
			return
		}
	}

	if !found {
		glog.V(4).Infof("creating forwardChain...")
		err := c.ipt.NewChain(FilterTable, c.forwardChainName)
		if err != nil {
			glog.Errorf("couldn't create forwardChain, see: %v", err)
			c.countError()
			return
		}
		glog.V(4).Infof("created forwardChain")
	}
}

func (c *Controller) getSrcForwardRuleStringForEndpointAndProt(endpoint Endpoint, prot Protocol) string {
	// iptables -t filter -A FORWARD -s 10.0.0.2 --sport 1234 -j ACCEPT
	return fmt.Sprintf("-p %s -s %s --sport %d -j ACCEPT", prot.String(), endpoint.IP.String(), endpoint.Port)
}

func (c *Controller) getDstForwardRuleStringForEndpointAndProt(endpoint Endpoint, prot Protocol) string {
	// iptables -t filter -A FORWARD -d 10.0.0.2 --dport 1234 -j ACCEPT
	return fmt.Sprintf("-p %s -d %s --dport %d -j ACCEPT", prot.String(), endpoint.IP.String(), endpoint.Port)
}

func (c *Controller) ensureForwardChainEntries(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	// Iterate over all lbs (in config) and ensure forward entries for every output
	rules, err := c.ipt.List(FilterTable, c.forwardChainName)
	if err != nil {
		glog.Errorf("couldn't retrieve rules in forwardChain `%s`, see: %v", c.forwardChainName, err)
		c.countError()
		return
	}

	for lbKey, lb := range c.loadbalancers {
		for _, output := range lb.Outputs {
			srcRule := c.getSrcForwardRuleStringForEndpointAndProt(output, lb.Protocol)
			if !c.rulesContainRule(rules, srcRule) {
				err = c.ipt.Append(FilterTable, c.forwardChainName, strings.Split(srcRule, " ")...)
				if err != nil {
					glog.Errorf("couldn't create source forward rule for output `%s` of lb `%s`, see: %v", output.String(), lbKey, err)
					c.countError()
				} else {
					glog.Infof("added source forward rule for output `%s` of lb `%s`", output.String(), lbKey)
				}
			}

			dstRule := c.getDstForwardRuleStringForEndpointAndProt(output, lb.Protocol)
			if !c.rulesContainRule(rules, dstRule) {
				err = c.ipt.Append(FilterTable, c.forwardChainName, strings.Split(dstRule, " ")...)
				if err != nil {
					glog.Errorf("couldn't create destination forward rule for output `%s` of lb `%s`, see: %v", output.String(), lbKey, err)
					c.countError()
				} else {
					glog.V(4).Infof("added destination forward rule for output `%s` of lb `%s`", output.String(), lbKey)
				}
			}
		}
	}
}

func (c *Controller) deleteObsoleteForwardChainEntries(allChains []string, chainIDs []ChainID, lbToChains map[string][]ChainID) {
	// Delete everything not referenced by any NAT chain (so in case we couldnt create new outputs, the old ones (not in config anymore) can still accept traffic)
	forwardRules, err := c.ipt.List(FilterTable, c.forwardChainName)
	if err != nil {
		glog.Errorf("couldn't retrieve rules in forwardChain `%s`, see: %v", c.forwardChainName, err)
		c.countError()
		return
	}

	referencedEndpoints := make(map[string]struct{})

	for _, chainID := range chainIDs {
		rulesInChain, err := c.ipt.List(NATTable, chainID.String())
		if err != nil {
			glog.Errorf("WILL NOT DELETE ANY OBSOLETE FORWARD CHAIN ENTRIES, see: couldn't retrieve rules in chain `%s`, see: %v", chainID.String(), err)
			c.countError()
			return
		}

		for _, rule := range rulesInChain {
			if rule == "-N "+chainID.String() {
				continue
			}

			dest, err := c.getDestinationFromRule(rule)
			if err != nil {
				glog.Errorf("WILL NOT DELETE ANY OBSOLETE FORWARD CHAIN ENTRIES, see: couldn't find endpoint in rule `%s`, see: %v", rule, err)
				c.countError()
				return
			}

			referencedEndpoints[dest.String()] = struct{}{}
		}
	}

	for _, rule := range forwardRules {
		rule = c.stripNARules(rule)

		if rule == "" {
			// e.g. -N or -A rule
			continue
		}

		dest, err := c.getDestinationFromForwardRule(rule)
		if err != nil {
			glog.Errorf("can't delete potential obsolete forward chain entry, see: couldn't get destination from forward rule `%s`, see: %v", rule, err)
			c.countError()
			continue
		}

		_, isReferenced := referencedEndpoints[dest.String()]
		if !isReferenced {
			// Fuckly hack since iptables gives us the mask, but doesnt like it when we give it...
			rule = strings.ReplaceAll(rule, dest.IP.String()+"/32", dest.IP.String())

			err := c.ipt.Delete(FilterTable, c.forwardChainName, strings.Split(rule, " ")...)
			if err != nil {
				glog.Errorf("couldn't delete obsolete forward rule `%s`, see: %v", rule, err)
				c.countError()
				continue
			}

			glog.V(4).Infof("deleted obsolete forward rule `%s`", rule)
		}
	}
}
