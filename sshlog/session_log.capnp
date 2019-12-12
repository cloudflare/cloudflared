using Go = import "go.capnp";
@0x8f43375162194466;
$Go.package("sshlog");
$Go.import("github.com/cloudflare/cloudflared/sshlog");

struct SessionLog {
    timestamp @0 :Text;
    content @1 :Data;
}