// Copyright 2019 Authors of Cilium
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build privileged_tests

package linux

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/cilium/cilium/pkg/bpf"
	"github.com/cilium/cilium/pkg/datapath"
	"github.com/cilium/cilium/pkg/datapath/loader"
	"github.com/cilium/cilium/pkg/node"
	"github.com/cilium/cilium/pkg/testutils"

	. "gopkg.in/check.v1"
)

type DatapathSuite struct{}

var (
	_ = Suite(&DatapathSuite{})

	dummyNodeCfg  = datapath.LocalNodeConfiguration{}
	dummyDevCfg   = testutils.NewTestEndpoint()
	dummyEPCfg    = testutils.NewTestEndpoint()
	ipv4DummyAddr = []byte{192, 0, 2, 3}
)

func (s *DatapathSuite) SetUpTest(c *C) {
	err := bpf.ConfigureResourceLimits()
	c.Assert(err, IsNil)
	node.InitDefaultPrefix("")
	node.SetInternalIPv4(ipv4DummyAddr)
	node.SetIPv4Loopback(ipv4DummyAddr)
}

func (s *DatapathSuite) TearDownTest(c *C) {
	node.SetInternalIPv4(nil)
	node.SetIPv4Loopback(nil)
}

type badWriter struct{}

func (b *badWriter) Write(p []byte) (int, error) {
	return 0, errors.New("bad write :(")
}

type writeFn func(io.Writer, datapath.Datapath) error

func writeConfig(c *C, header string, write writeFn) {
	tests := []struct {
		description string
		output      io.Writer
		expResult   Checker
	}{
		{
			description: "successful write to an in-memory buffer",
			output:      &bytes.Buffer{},
			expResult:   IsNil,
		},
		{
			description: "write to a failing writer",
			output:      &badWriter{},
			expResult:   NotNil,
		},
	}
	for _, test := range tests {
		c.Logf("  Testing %s configuration: %s", header, test.description)
		dp := NewDatapath(DatapathConfiguration{}, nil)
		c.Assert(write(test.output, dp), test.expResult)
	}
}

func (s *DatapathSuite) TestWriteNodeConfig(c *C) {
	writeConfig(c, "node", func(w io.Writer, dp datapath.Datapath) error {
		return dp.WriteNodeConfig(w, &dummyNodeCfg)
	})
}

func (s *DatapathSuite) TestWriteNetdevConfig(c *C) {
	writeConfig(c, "netdev", func(w io.Writer, dp datapath.Datapath) error {
		return dp.WriteNetdevConfig(w, &dummyDevCfg)
	})
}

func (s *DatapathSuite) TestWriteEndpointConfig(c *C) {
	writeConfig(c, "endpoint", func(w io.Writer, dp datapath.Datapath) error {
		return dp.WriteEndpointConfig(w, &dummyEPCfg)
	})
}

func (s *DatapathSuite) TestWriteStaticData(c *C) {
	dp := NewDatapath(DatapathConfiguration{}, nil).(*linuxDatapath)
	ep := &dummyEPCfg

	varSub, stringSub := loader.ELFSubstitutions(ep)

	var buf bytes.Buffer
	dp.writeStaticData(&buf, ep)
	b := buf.Bytes()
	for k := range varSub {
		for _, suffix := range []string{"_1", "_2", "_3", "_4"} {
			// Variables with these suffixes are implemented via
			// multiple 32-bit values. The header define doesn't
			// include these numbers though, so strip them.
			if strings.HasSuffix(k, suffix) {
				k = strings.TrimSuffix(k, suffix)
				break
			}
		}
		c.Assert(bytes.Contains(b, []byte(k)), Equals, true)
	}
	for _, v := range stringSub {
		c.Logf("Ensuring config has %s", v)
		if strings.HasPrefix(v, "1/0x") {
			// Skip tail call map name replacement
			continue
		}
		c.Assert(bytes.Contains(b, []byte(v)), Equals, true)
	}
}
