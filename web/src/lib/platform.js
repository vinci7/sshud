import { BareMetalServer, BareMetalServer_01, Cloud } from '@carbon/icons-react';
import {
  siAlmalinux, siAlpinelinux, siArchlinux, siCentos, siDebian, siFedora, siLinuxmint,
  siOpensuse, siProxmox, siRaspberrypi, siRockylinux, siUbuntu,
} from 'simple-icons'; // CC0; imported by name so the bundle keeps only these

// Distro logos by os-release ID. Oracle Linux ("ol") has no fitting mark;
// Proxmox VE keeps Debian's os-release, so it is told by its -pve kernel.
const DISTROS = {
  ubuntu: siUbuntu, debian: siDebian, alpine: siAlpinelinux, centos: siCentos, rocky: siRockylinux,
  almalinux: siAlmalinux, fedora: siFedora, arch: siArchlinux, opensuse: siOpensuse,
  'opensuse-leap': siOpensuse, 'opensuse-tumbleweed': siOpensuse, linuxmint: siLinuxmint,
};

// Cloud vendor kernels end in the vendor's name; -kvm and Debian's
// -cloud-amd64 / -cloud-arm64 are generic cloud flavours.
const CLOUDS = { oracle: 'Oracle Cloud', aws: 'AWS', gcp: 'Google Cloud', azure: 'Azure', ibm: 'IBM Cloud', kvm: '', cloud: '' };

// platform says what the machine is (not just what it runs), first match wins:
// 1. Raspberry Pi: Raspbian, a Pi kernel, or an ARM Debian calling itself Raspbian
// 2. Synology NAS: DSM
// 3. Cloud server: a vendor kernel (the label names the vendor; generic ones have none)
// 4. Proxmox by kernel, else the distribution logo by os-release ID (no label: the subtitle already says it)
// 5. a generic server
// icon is a Carbon icon component or a Simple Icons object ({ path }).
export function platform(s) {
  const { kernel = '', osId = '', osLike = '', os = '', arch = '' } = s?.host ?? {};
  if (
    osId === 'raspbian' ||
    /raspi|rpi|-v7l\+|-v8\+|-v7\+/.test(kernel) ||
    (/^(armv7l|aarch64)$/.test(arch) && /debian/.test(osLike) && /raspbian/i.test(os))
  )
    return { icon: siRaspberrypi, label: 'Raspberry Pi' };
  if (osId === 'dsm') return { icon: BareMetalServer, label: 'Synology' };
  const cloud = kernel.match(/-(oracle|aws|gcp|azure|ibm|kvm|cloud)(-amd64|-arm64)?$/)?.[1];
  if (cloud) return { icon: Cloud, label: CLOUDS[cloud] };
  if (kernel.endsWith('-pve')) return { icon: siProxmox, label: '' };
  return { icon: DISTROS[osId] ?? BareMetalServer_01, label: '' };
}
