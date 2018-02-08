package main

import (
	"fmt"
	"io/ioutil"
	"log"

	"github.com/mkideal/cli"
	"github.com/mlctrez/ifacemaker/maker"
)

type cmdlineArgs struct {
	cli.Helper
	Files      []string `cli:"*f,file"      usage:"Go source file or directory to read"`
	StructType string   `cli:"*s,struct"    usage:"Generate an interface for this structure name"`
	IfaceName  string   `cli:"*i,iface"     usage:"Name of the generated interface"`
	PkgName    string   `cli:"*p,pkg"       usage:"Package name for the generated interface"`
	CopyDocs   bool     `cli:"d,doc"        usage:"Copy method documentation from source files." dft:"true"`
	Output     string   `cli:"o,output"     usage:"Output file name. If not provided, result will be printed to stdout."`
	AddImport  string   `cli:"a,add-import" usage:"An additional import to add to the generated file."`
	Rewrite    string   `cli:"r,rewrite"    usage:"Rewrites unqualified exports with this package prefix."`
}

func Run(args *cmdlineArgs) {
	maker := &maker.Maker{
		StructName: args.StructType,
		CopyDocs:   args.CopyDocs,
	}
	if args.AddImport != "" {
		maker.AddImport("", args.AddImport)
	}
	if args.Rewrite != "" {
		maker.SourcePackage(args.Rewrite)
	}

	allFiles, err := maker.GetGoFiles(args.Files...)
	if err != nil {
		log.Fatal(err.Error())
	}

	err = maker.ParseFiles(allFiles...)
	if err != nil {
		log.Fatal(err.Error())
	}

	result, err := maker.MakeInterface(args.PkgName, args.IfaceName)
	if err != nil {
		log.Fatal(err.Error())
	}

	if args.Output == "" {
		fmt.Println(string(result))
	} else {
		ioutil.WriteFile(args.Output, result, 0644)
	}

}

func main() {
	cli.Run(&cmdlineArgs{}, func(ctx *cli.Context) error {
		argv := ctx.Argv().(*cmdlineArgs)
		Run(argv)
		return nil
	})
}
