# Generate go.capnp.out with:
# capnp compile -o- go.capnp > go.capnp.out
# Must run inside this directory to preserve paths.

@0xd12a1c51fedd6c88;

annotation package(file) :Text;
annotation import(file) :Text;
annotation doc(struct, field, enum) :Text;
annotation tag(enumerant) :Text;
annotation notag(enumerant) :Void;
annotation customtype(field) :Text;
annotation name(struct, field, union, enum, enumerant, interface, method, param, annotation, const, group) :Text;

$package("capnp");
