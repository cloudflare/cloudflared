# Cloudflared SSH server smoke tests

Runs several tests in a docker container against a server that is started out of band of these tests.
Cloudflared token also needs to be retrieved out of band.
SSH server hostname and user need to be configured in a docker environment file


## Running tests

* Build cloudflared:
make cloudflared

* Start server:
sudo ./cloudflared tunnel --hostname HOSTNAME --ssh-server

* Fetch token: 
./cloudflared access login HOSTNAME

* Create docker env file:
echo "SSH_HOSTNAME=HOSTNAME\nSSH_USER=USERNAME\n" > ssh_server_tests/.env

* Run tests:
make test-ssh-server
