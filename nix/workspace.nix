{ pkgs, lib, ... }:
{
  system.stateVersion = "26.05";

  networking.hostName = "workspace";
  networking.enableIPv6 = false;
  networking.firewall.enable = true;
  networking.nameservers = [ "1.1.1.1" "9.9.9.9" ];

  virtualisation.incus.agent.enable = true;

  services.openssh.enable = false;

  # The upstream Incus image module intentionally permits passwordless root
  # console login. WebNix does not need that behavior, so lock root explicitly.
  users.users.root.initialHashedPassword = lib.mkForce "!";

  users.groups.dev.gid = 1000;
  users.users.dev = {
    isNormalUser = true;
    uid = 1000;
    group = "dev";
    extraGroups = [ "wheel" ];
    home = "/home/dev";
    createHome = true;
    shell = pkgs.bashInteractive;
  };

  security.sudo.wheelNeedsPassword = false;

  nix = {
    settings = {
      experimental-features = [ "nix-command" "flakes" ];
      sandbox = true;
      builders-use-substitutes = true;
      warn-dirty = false;
    };
    gc = {
      automatic = true;
      dates = "weekly";
      options = "--delete-older-than 14d";
    };
  };

  programs.direnv = {
    enable = true;
    nix-direnv.enable = true;
  };

  environment.systemPackages = with pkgs; [
    bashInteractive
    git
    curl
    wget
    jq
    ripgrep
    fd
    tmux
    htop
    vim
    nano
    file
    unzip
    zip
  ];

  environment.variables = {
    EDITOR = "vim";
  };

  boot.kernel.sysctl = {
    "kernel.kptr_restrict" = 2;
    "kernel.dmesg_restrict" = 1;
  };

  documentation.enable = false;
  services.getty.autologinUser = lib.mkForce null;
}
