# ifacemaker

This is a development helper program that generates a Golang interface by inspecting
the structure methods of an existing `.go` files. The primary use case is to generate
interfaces for gomock, so that gomock can generate mocks from those interfaces. This
makes unit testing easier.

## Origins

* https://github.com/vburenin/ifacemaker
* https://github.com/nkovacs/ifacemaker

## Install

```
go get github.com/mlctrez/ifacemaker
```

## Usage
Here is the help output of ifacemaker:

```
$ ifacemaker --help
Options:
  
  -h, --help         display help information
  -f, --file        *Go source file or directory to read
  -s, --struct      *Generate an interface for this structure name
  -i, --iface       *Name of the generated interface
  -p, --pkg         *Package name for the generated interface
  -d, --doc[=true]   Copy method documentation from source files.
  -o, --output       Output file name. If not provided, result will be printed to stdout.
  -a, --add-import   An additional import to add to the generated file.
  -r, --rewrite      Rewrites unqualified exports with this package prefix.
$
```

As an example, let's say you wanted to generate an interface for the Human structure
in this sample code:

```
package main

import "fmt"

type Human struct {
	name string
	age  int
}

// Returns the name of our Human.
func (h *Human) GetName() string {
	return h.name
}

// Our Human just had a birthday! Increase its age.
func (h *Human) Birthday() {
	h.age += 1
	fmt.Printf("I am now %d years old!\n", h.age)
}

// Make the Human say hello.
func (h *Human) SayHello() {
	fmt.Printf("Hello, my name is %s, and I am %d years old.\n", h.name, h.age)
}

func main() {
	human := &Human{name: "Bob", age: 30}
	human.GetName()
	human.SayHello()
	human.Birthday()
}
```

The ifacemaker helper program can generate this interface for you:

```
$ ifacemaker -f human.go -s Human -i HumanIface -p humantest
package humantest

type HumanIface interface {
	// Returns the name of our Human.
	GetName() string
	// Our Human just had a birthday! Increase its age.
	Birthday()
	// Make the Human say hello.
	SayHello()
}

$
```

In the above example, ifacemaker inspected the structure methods of the Human struct
and generated an interface called HumanIface in the humantest package. Note that the
ifacemaker program preserves docstrings by default.

You can tell ifacemaker to write its output to a file, versus stdout, using the `-o`
parameter:

```
$ ifacemaker -f human.go -s Human -i HumanIface -p humantest -o humaniface.go
$
```

Field and return types in the generated interface can be re-written to include the source package name.
See `scripts/sample.sh` for example usage of the `--add-import` and `--rewrite` args.
