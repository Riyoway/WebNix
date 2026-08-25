{ config, lib, pkgs, ... }:
let
  incusPkg = config.virtualisation.incus.package;
in
{
  assertions = [
    {
      assertion = lib.versionAtLeast incusPkg.version "7.3.0";
      message = "WebNix multi-tenant deployments require Incus >= 7.3.0.";
    }
  ];

  virtualisation.incus = {
    enable = true;
    package = pkgs.incus;
  };

  networking.nftables.enable = true;
  networking.nftables.flushRuleset = false;

  services.tailscale = {
    enable = true;
    openFirewall = true;
  };

  networking.firewall.allowedTCPPorts = [ 80 443 ];

  # Public SSH is intentionally not enabled here. Use Tailscale SSH or another
  # private administration path for the host.

  boot.kernel.sysctl = {
    "kernel.kptr_restrict" = 2;
    "kernel.dmesg_restrict" = 1;
    "fs.protected_hardlinks" = 1;
    "fs.protected_symlinks" = 1;
    "fs.protected_fifos" = 2;
    "fs.protected_regular" = 2;
  };
}
