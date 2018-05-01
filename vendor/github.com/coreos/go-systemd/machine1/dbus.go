/*
Copyright 2015 CoreOS Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Integration with the systemd machined API.  See http://www.freedesktop.org/wiki/Software/systemd/machined/
package machine1

import (
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/godbus/dbus"
)

const (
	dbusInterface = "org.freedesktop.machine1.Manager"
	dbusPath      = "/org/freedesktop/machine1"
)

// Conn is a connection to systemds dbus endpoint.
type Conn struct {
	conn   *dbus.Conn
	object dbus.BusObject
}

// New() establishes a connection to the system bus and authenticates.
func New() (*Conn, error) {
	c := new(Conn)

	if err := c.initConnection(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Conn) initConnection() error {
	var err error
	c.conn, err = dbus.SystemBusPrivate()
	if err != nil {
		return err
	}

	// Only use EXTERNAL method, and hardcode the uid (not username)
	// to avoid a username lookup (which requires a dynamically linked
	// libc)
	methods := []dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Getuid()))}

	err = c.conn.Auth(methods)
	if err != nil {
		c.conn.Close()
		return err
	}

	err = c.conn.Hello()
	if err != nil {
		c.conn.Close()
		return err
	}

	c.object = c.conn.Object("org.freedesktop.machine1", dbus.ObjectPath(dbusPath))

	return nil
}

func (c *Conn) getPath(method string, args ...interface{}) (dbus.ObjectPath, error) {
	result := c.object.Call(fmt.Sprintf("%s.%s", dbusInterface, method), 0, args...)
	if result.Err != nil {
		return "", result.Err
	}

	path, typeErr := result.Body[0].(dbus.ObjectPath)
	if !typeErr {
		return "", fmt.Errorf("unable to convert dbus response '%v' to dbus.ObjectPath", result.Body[0])
	}

	return path, nil
}

// GetMachine gets a specific container with systemd-machined
func (c *Conn) GetMachine(name string) (dbus.ObjectPath, error) {
	return c.getPath("GetMachine", name)
}

// GetImage gets a specific image with systemd-machined
func (c *Conn) GetImage(name string) (dbus.ObjectPath, error) {
	return c.getPath("GetImage", name)
}

// GetMachineByPID gets a machine specified by a PID from systemd-machined
func (c *Conn) GetMachineByPID(pid uint) (dbus.ObjectPath, error) {
	return c.getPath("GetMachineByPID", pid)
}

// DescribeMachine gets the properties of a machine
func (c *Conn) DescribeMachine(name string) (machineProps map[string]interface{}, err error) {
	var dbusProps map[string]dbus.Variant
	path, pathErr := c.GetMachine(name)
	if pathErr != nil {
		return nil, pathErr
	}
	obj := c.conn.Object("org.freedesktop.machine1", path)
	err = obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "").Store(&dbusProps)
	if err != nil {
		return nil, err
	}
	machineProps = make(map[string]interface{}, len(dbusProps))
	for key, val := range dbusProps {
		machineProps[key] = val.Value()
	}
	return
}

// KillMachine sends a signal to a machine
func (c *Conn) KillMachine(name, who string, sig syscall.Signal) error {
	return c.object.Call(dbusInterface+".KillMachine", 0, name, who, sig).Err
}

// TerminateMachine causes systemd-machined to terminate a machine, killing its processes
func (c *Conn) TerminateMachine(name string) error {
	return c.object.Call(dbusInterface+".TerminateMachine", 0, name).Err
}

// RegisterMachine registers the container with the systemd-machined
func (c *Conn) RegisterMachine(name string, id []byte, service string, class string, pid int, root_directory string) error {
	return c.object.Call(dbusInterface+".RegisterMachine", 0, name, id, service, class, uint32(pid), root_directory).Err
}
