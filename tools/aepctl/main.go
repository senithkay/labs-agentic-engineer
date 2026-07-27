// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package main

import (
	_ "embed"

	"github.com/wso2/aep/aepctl/cmd"
)

// configTemplate is the annotated, defaults-filled config file that
// `aep platform config init` writes out. Embedded so a freshly-downloaded
// binary can produce a starter config with no cluster and no repo checkout.
//
//go:embed config.example.yaml
var configTemplate string

func main() {
	cmd.ConfigTemplate = configTemplate
	cmd.Execute()
}
