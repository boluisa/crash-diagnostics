// Copyright (c) 2019 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package exec

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vmware-tanzu/crash-diagnostics/archiver"

	"github.com/sirupsen/logrus"
	"github.com/vmware-tanzu/crash-diagnostics/script"
)

// Executor represents a type that can execute a script
type Executor struct {
	script *script.Script
}

// New returns an *Executor
func New(src *script.Script) *Executor {
	return &Executor{script: src}
}

// Execute executes the configured script
func (e *Executor) Execute() error {
	logrus.Info("Executing script file")

	asCmd, err := exeAs(e.script)
	if err != nil {
		return err
	}

	// execute ENVs, store all declared env values in
	// running process enviroment variables.
	if err := exeEnvs(e.script); err != nil {
		return fmt.Errorf("exec: %s", err)
	}

	// exec FROM
	fromCmd, err := exeFrom(e.script)
	if err != nil {
		return err
	}

	// exec WORKDIR
	workdir, err := exeWorkdir(e.script)
	if err != nil {
		return err
	}

	// exec OUTPUT
	output, err := exeOutput(e.script)
	if err != nil {
		return err
	}

	// attempt to create client from KUBECONFIG
	k8sClient, err := exeKubeConfig(e.script)
	if err != nil {
		logrus.Warnf("Failed to load KUBECONFIG: %s", err)
	}

	// Execute each action as appeared in script
	authCmd, err := exeAuthConfig(e.script)
	if err != nil {
		return err
	}

	for _, action := range e.script.Actions {
		switch cmd := action.(type) {
		case *script.KubeGetCommand:
			logrus.Infof("KUBEGET: getting API objects (this may take a while)")
			if err := exeKubeGet(k8sClient, cmd, workdir.Path()); err != nil {
				return fmt.Errorf("KUBEGET: %s", err)
			}
		default:
			for _, node := range fromCmd.Nodes() {
				nodeWorkdir, err := makeNodeWorkdir(workdir.Path(), node)
				if err != nil {
					return err
				}

				logrus.Debugf("Executing command %s/%s: ", node.Address(), cmd.Name())
				if err := cmdExec(asCmd, authCmd, action, &node, nodeWorkdir); err != nil {
					return err
				}
			}
		}
	}

	// write result to output
	if err := archiver.Tar(output.Path(), workdir.Path()); err != nil {
		return err
	}
	logrus.Infof("Created output at path %s", output.Path())
	logrus.Info("Done")

	return nil
}

func makeNodeWorkdir(workdir string, machine script.Node) (string, error) {
	machineAddr := machine.Address()
	machineWorkdir := filepath.Join(workdir, sanitizeStr(machineAddr))
	if err := os.MkdirAll(machineWorkdir, 0744); err != nil && !os.IsExist(err) {
		return "", err
	}
	return machineWorkdir, nil
}
