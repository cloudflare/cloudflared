"""
Cloudflared Integration tests
"""

import unittest
import subprocess
import os
import tempfile
from contextlib import contextmanager

from pexpect import pxssh


class TestSSHBase(unittest.TestCase):
    """
    SSH test base class containing constants and helper funcs
    """

    HOSTNAME = os.environ["SSH_HOSTNAME"]
    SSH_USER = os.environ["SSH_USER"]
    SSH_TARGET = f"{SSH_USER}@{HOSTNAME}"
    AUTHORIZED_KEYS_SSH_CONFIG = os.environ["AUTHORIZED_KEYS_SSH_CONFIG"]
    SHORT_LIVED_CERT_SSH_CONFIG = os.environ["SHORT_LIVED_CERT_SSH_CONFIG"]
    SSH_OPTIONS = {"StrictHostKeyChecking": "no"}

    @classmethod
    def get_ssh_command(cls, pty=True):
        """
        Return ssh command arg list. If pty is true, a PTY is forced for the session.
        """
        cmd = [
            "ssh",
            "-o",
            "StrictHostKeyChecking=no",
            "-F",
            cls.AUTHORIZED_KEYS_SSH_CONFIG,
            cls.SSH_TARGET,
        ]
        if not pty:
            cmd += ["-T"]
        else:
            cmd += ["-tt"]

        return cmd

    @classmethod
    @contextmanager
    def ssh_session_manager(cls, *args, **kwargs):
        """
        Context manager for interacting with a pxssh session.
        Disables pty echo on the remote server and ensures session is terminated afterward.
        """
        session = pxssh.pxssh(options=cls.SSH_OPTIONS)

        session.login(
            cls.HOSTNAME,
            username=cls.SSH_USER,
            original_prompt=r"[#@$]",
            ssh_config=kwargs.get("ssh_config", cls.AUTHORIZED_KEYS_SSH_CONFIG),
            ssh_tunnels=kwargs.get("ssh_tunnels", {}),
        )
        try:
            session.sendline("stty -echo")
            session.prompt()
            yield session
        finally:
            session.logout()

    @staticmethod
    def get_command_output(session, cmd):
        """
        Executes command on remote ssh server and waits for prompt.
        Returns command output
        """
        session.sendline(cmd)
        session.prompt()
        return session.before.decode().strip()

    def exec_command(self, cmd, shell=False):
        """
        Executes command locally. Raises Assertion error for non-zero return code.
        Returns stdout and stderr
        """
        proc = subprocess.Popen(
            cmd, stderr=subprocess.PIPE, stdout=subprocess.PIPE, shell=shell
        )
        raw_out, raw_err = proc.communicate()

        out = raw_out.decode()
        err = raw_err.decode()
        self.assertEqual(proc.returncode, 0, msg=f"stdout: {out} stderr: {err}")
        return out.strip(), err.strip()


class TestSSHCommandExec(TestSSHBase):
    """
    Tests inline ssh command exec
    """

    # Name of file to be downloaded over SCP on remote server.
    REMOTE_SCP_FILENAME = os.environ["REMOTE_SCP_FILENAME"]

    @classmethod
    def get_scp_base_command(cls):
        return [
            "scp",
            "-o",
            "StrictHostKeyChecking=no",
            "-v",
            "-F",
            cls.AUTHORIZED_KEYS_SSH_CONFIG,
        ]

    @unittest.skip(
        "This creates files on the remote. Should be skipped until server is dockerized."
    )
    def test_verbose_scp_sink_mode(self):
        with tempfile.NamedTemporaryFile() as fl:
            self.exec_command(
                self.get_scp_base_command() + [fl.name, f"{self.SSH_TARGET}:"]
            )

    def test_verbose_scp_source_mode(self):
        with tempfile.TemporaryDirectory() as tmpdirname:
            self.exec_command(
                self.get_scp_base_command()
                + [f"{self.SSH_TARGET}:{self.REMOTE_SCP_FILENAME}", tmpdirname]
            )
            local_filename = os.path.join(tmpdirname, self.REMOTE_SCP_FILENAME)

            self.assertTrue(os.path.exists(local_filename))
            self.assertTrue(os.path.getsize(local_filename) > 0)

    def test_pty_command(self):
        base_cmd = self.get_ssh_command()

        out, _ = self.exec_command(base_cmd + ["whoami"])
        self.assertEqual(out.strip().lower(), self.SSH_USER.lower())

        out, _ = self.exec_command(base_cmd + ["tty"])
        self.assertNotEqual(out, "not a tty")

    def test_non_pty_command(self):
        base_cmd = self.get_ssh_command(pty=False)

        out, _ = self.exec_command(base_cmd + ["whoami"])
        self.assertEqual(out.strip().lower(), self.SSH_USER.lower())

        out, _ = self.exec_command(base_cmd + ["tty"])
        self.assertEqual(out, "not a tty")


class TestSSHShell(TestSSHBase):
    """
    Tests interactive SSH shell
    """

    # File path to a file on the remote server with root only read privileges.
    ROOT_ONLY_TEST_FILE_PATH = os.environ["ROOT_ONLY_TEST_FILE_PATH"]

    def test_ssh_pty(self):
        with self.ssh_session_manager() as session:

            # Test shell launched as correct user
            username = self.get_command_output(session, "whoami")
            self.assertEqual(username.lower(), self.SSH_USER.lower())

            # Test USER env variable set
            user_var = self.get_command_output(session, "echo $USER")
            self.assertEqual(user_var.lower(), self.SSH_USER.lower())

            # Test HOME env variable set to true user home.
            home_env = self.get_command_output(session, "echo $HOME")
            pwd = self.get_command_output(session, "pwd")
            self.assertEqual(pwd, home_env)

            # Test shell launched in correct user home dir.
            self.assertIn(username, pwd)

            # Ensure shell launched with correct user's permissions and privs.
            # Can't read root owned 0700 files.
            output = self.get_command_output(
                session, f"cat {self.ROOT_ONLY_TEST_FILE_PATH}"
            )
            self.assertIn("Permission denied", output)

    def test_short_lived_cert_auth(self):
        with self.ssh_session_manager(
            ssh_config=self.SHORT_LIVED_CERT_SSH_CONFIG
        ) as session:
            username = self.get_command_output(session, "whoami")
            self.assertEqual(username.lower(), self.SSH_USER.lower())


unittest.main()
