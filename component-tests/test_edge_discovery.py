import ipaddress
import socket

import pytest

from constants import protocols
from cli import CloudflaredCli
from util import get_tunnel_connector_id, LOGGER, wait_tunnel_ready, write_config


class TestEdgeDiscovery:
    def _extra_config(self, protocol, edge_ip_version):
        config = {
            "protocol": protocol,
        }
        if edge_ip_version:
            config["edge-ip-version"] = edge_ip_version
        return config

    @pytest.mark.parametrize("protocol", protocols())
    def test_default_only(self, tmp_path, component_tests_config, protocol):
        """
        This test runs a tunnel to connect via IPv4-only edge addresses (default is unset "--edge-ip-version 4")
        """
        if self.has_ipv6_only():
            pytest.skip("Host has IPv6 only support and current default is IPv4 only")
        self.expect_address_connections(
            tmp_path, component_tests_config, protocol, None, self.expect_ipv4_address)

    @pytest.mark.parametrize("protocol", protocols())
    def test_ipv4_only(self, tmp_path, component_tests_config, protocol):
        """
        This test runs a tunnel to connect via IPv4-only edge addresses
        """
        if self.has_ipv6_only():
            pytest.skip("Host has IPv6 only support")
        self.expect_address_connections(
            tmp_path, component_tests_config, protocol, "4", self.expect_ipv4_address)

    @pytest.mark.parametrize("protocol", protocols())
    def test_ipv6_only(self, tmp_path, component_tests_config, protocol):
        """
        This test runs a tunnel to connect via IPv6-only edge addresses
        """
        if self.has_ipv4_only():
            pytest.skip("Host has IPv4 only support")
        self.expect_address_connections(
            tmp_path, component_tests_config, protocol, "6", self.expect_ipv6_address)

    @pytest.mark.parametrize("protocol", protocols())
    def test_auto_ip64(self, tmp_path, component_tests_config, protocol):
        """
        This test runs a tunnel to connect via auto with a preference of IPv6 then IPv4 addresses for a dual stack host

        This test also assumes that the host has IPv6 preference.
        """
        if not self.has_dual_stack(address_family_preference=socket.AddressFamily.AF_INET6):
            pytest.skip("Host does not support dual stack with IPv6 preference")
        self.expect_address_connections(
            tmp_path, component_tests_config, protocol, "auto", self.expect_ipv6_address)

    @pytest.mark.parametrize("protocol", protocols())
    def test_auto_ip46(self, tmp_path, component_tests_config, protocol):
        """
        This test runs a tunnel to connect via auto with a preference of IPv4 then IPv6 addresses for a dual stack host

        This test also assumes that the host has IPv4 preference.
        """
        if not self.has_dual_stack(address_family_preference=socket.AddressFamily.AF_INET):
            pytest.skip("Host does not support dual stack with IPv4 preference")
        self.expect_address_connections(
            tmp_path, component_tests_config, protocol, "auto", self.expect_ipv4_address)

    def expect_address_connections(self, tmp_path, component_tests_config, protocol, edge_ip_version, assert_address_type):
        config = component_tests_config(
            self._extra_config(protocol, edge_ip_version))
        config_path = write_config(tmp_path, config.full_config)
        LOGGER.debug(config)
        with CloudflaredCli(config, config_path, LOGGER):
            wait_tunnel_ready(tunnel_url=config.get_url(),
                              require_min_connections=4)
            cfd_cli = CloudflaredCli(config, config_path, LOGGER)
            tunnel_id = config.get_tunnel_id()
            info = cfd_cli.get_tunnel_info(tunnel_id)
            connector_id = get_tunnel_connector_id()
            connector = next(
                (c for c in info["conns"] if c["id"] == connector_id), None)
            assert connector, f"Expected connection info from get tunnel info for the connected instance: {info}"
            conns = connector["conns"]
            assert conns == None or len(
                conns) == 4, f"There should be 4 connections registered: {conns}"
            for conn in conns:
                origin_ip = conn["origin_ip"]
                assert origin_ip, f"No available origin_ip for this connection: {conn}"
                assert_address_type(origin_ip)

    def expect_ipv4_address(self, address):
        assert type(ipaddress.ip_address(
            address)) is ipaddress.IPv4Address, f"Expected connection from origin to be a valid IPv4 address: {address}"

    def expect_ipv6_address(self, address):
        assert type(ipaddress.ip_address(
            address)) is ipaddress.IPv6Address, f"Expected connection from origin to be a valid IPv6 address: {address}"

    def get_addresses(self):
        """
        Returns a list of addresses for the host.
        """
        host_addresses = socket.getaddrinfo(
            "region1.v2.argotunnel.com", 7844, socket.AF_UNSPEC, socket.SOCK_STREAM)
        assert len(
            host_addresses) > 0, "No addresses returned from getaddrinfo"
        return host_addresses

    def has_dual_stack(self, address_family_preference=None):
        """
        Returns true if the host has dual stack support and can optionally check 
        the provided IP family preference.
        """
        dual_stack = not self.has_ipv6_only() and not self.has_ipv4_only()
        if address_family_preference:
            address = self.get_addresses()[0]
            return dual_stack and address[0] == address_family_preference

        return dual_stack

    def has_ipv6_only(self):
        """
        Returns True if the host has only IPv6 address support.
        """
        return self.attempt_connection(socket.AddressFamily.AF_INET6) and not self.attempt_connection(socket.AddressFamily.AF_INET)

    def has_ipv4_only(self):
        """
        Returns True if the host has only IPv4 address support.
        """
        return self.attempt_connection(socket.AddressFamily.AF_INET) and not self.attempt_connection(socket.AddressFamily.AF_INET6)

    def attempt_connection(self, address_family):
        """
        Returns True if a successful socket connection can be made to the 
        remote host with the provided address family to validate host support 
        for the provided address family.
        """
        address = None
        for a in self.get_addresses():
            if a[0] == address_family:
                address = a
                break
        if address is None:
            # Couldn't even lookup the address family so we can't connect
            return False
        af, socktype, proto, canonname, sockaddr = address
        s = None
        try:
            s = socket.socket(af, socktype, proto)
        except OSError:
            return False
        try:
            s.connect(sockaddr)
        except OSError:
            s.close()
            return False
        s.close()
        return True
