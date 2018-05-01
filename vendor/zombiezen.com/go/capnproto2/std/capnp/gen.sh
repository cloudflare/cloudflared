#!/bin/bash

infer_package_name() {
	# Convert the filename $1 to a package name. We munge the name as follows:
	#
	# 1. strip off the capnp file extension and dirname
	# 2. remove dashes
	# 3. convert '+' to 'x'. This is really just for c++.capnp, but it's not
	#    any easier to special case it.
	printf '%s' "$(basename $1)" | sed 's/\.capnp$// ; s/-//g ; s/+/x/g'
}

gen_annotated_schema() {
	# Copy the schema from file "$1" to the current directory, and add
	# appropriate $Go annotations.
	infile="$1"
	outfile="$(basename $infile)"
	cp "$infile" "$outfile" || return 1
	package_name="$(infer_package_name $outfile)"
	cat >> "$outfile" << EOF
using Go = import "/go.capnp";
\$Go.package("$package_name");
\$Go.import("zombiezen.com/go/capnproto2/std/capnp/$package_name");
EOF
}

gen_go_src() {
	# Generate go source code from the schema file $1. Create the package
	# directory if necessary.
	file="$1"
	package_name="$(infer_package_name $file)"
	mkdir -p $package_name || return 1
	capnp compile -I"$(dirname $PWD)" -ogo:$package_name $file
}

usage() {
	echo "Usage:"
	echo ""
	echo "    $0 import <path/to/schema/files>"
	echo "    $0 compile    # Generate go source files"
	echo "    $0 clean-go   # Remove go source files"
	echo "    $0 clean-all  # Remove go source files and imported schemas"
}

# do_* implements the corresponding subcommand described in usage's output.
do_import() {
	input_dir="$1"
	for file in $input_dir/*.capnp; do
		gen_annotated_schema "$file" || return 1
	done
}

do_compile() {
	for file in *.capnp; do
		gen_go_src "$file" || return 1
	done
}

do_clean_go() {
	find "$(dirname $0)" -name '*.capnp.go' -delete
	find "$(dirname $0)" -type d -empty -delete
}

do_clean_all() {
	do_clean_go
	find "$(dirname $0)" -name '*.capnp' -delete
}

eq_or_usage() {
	# If "$1" is not equal to "$2", call usage and exit.
	if [ ! $1 = $2 ] ; then
		usage
		exit 1
	fi
}

case "$1" in
	import)    eq_or_usage $# 2; do_import "$2" ;;
	compile)   eq_or_usage $# 1; do_compile ;;
	clean-go)  eq_or_usage $# 1; do_clean_go ;;
	clean-all) eq_or_usage $# 1; do_clean_all ;;
	*) usage; exit 1 ;;
esac
