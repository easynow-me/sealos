/**
 * Copyright (c) OpenLens Authors. All rights reserved.
 * Licensed under MIT License. See LICENSE in root directory for more information.
 */

import type { NamespaceScopedMetadata } from '../api-types';
import { KubeObject } from '../kube-object';

export interface GatewayServerPort {
  number: number;
  name: string;
  protocol: 'HTTP' | 'HTTPS' | 'GRPC' | 'HTTP2' | 'MONGO' | 'TCP' | 'TLS';
}

export interface GatewayServerTLS {
  mode: 'PASSTHROUGH' | 'SIMPLE' | 'MUTUAL' | 'AUTO_PASSTHROUGH' | 'ISTIO_MUTUAL';
  credentialName?: string;
  privateKey?: string;
  serverCertificate?: string;
  caCertificates?: string;
  subjectAltNames?: string[];
  httpsRedirect?: boolean;
}

export interface GatewayServer {
  port: GatewayServerPort;
  hosts: string[];
  tls?: GatewayServerTLS;
  name?: string;
}

export interface GatewaySelector {
  [key: string]: string;
}

export interface GatewaySpec {
  selector: GatewaySelector;
  servers: GatewayServer[];
}

export interface GatewayStatus {
  // Currently, Istio Gateway doesn't have a complex status
  conditions?: {
    type: string;
    status: string;
    reason?: string;
    message?: string;
    lastTransitionTime?: string;
  }[];
}

export class Gateway extends KubeObject<NamespaceScopedMetadata, GatewayStatus, GatewaySpec> {
  static readonly kind = 'Gateway';

  static readonly namespaced = true;

  static readonly apiBase = '/apis/networking.istio.io/v1beta1/gateways';

  getSelector(): GatewaySelector {
    return this.spec.selector || {};
  }

  getServers(): GatewayServer[] {
    return this.spec.servers || [];
  }

  getHosts(): string[] {
    return this.getServers().flatMap(server => server.hosts);
  }

  getPorts(): string {
    return this.getServers()
      .map(server => `${server.port.number}/${server.port.protocol}`)
      .join(', ');
  }

  getProtocols(): string[] {
    return [...new Set(this.getServers().map(server => server.port.protocol))];
  }

  getTLSInfo(): { hasTLS: boolean; modes: string[] } {
    const tlsModes = this.getServers()
      .filter(server => server.tls)
      .map(server => server.tls!.mode);

    return {
      hasTLS: tlsModes.length > 0,
      modes: [...new Set(tlsModes)]
    };
  }

  getCredentialNames(): string[] {
    return this.getServers()
      .filter(server => server.tls?.credentialName)
      .map(server => server.tls!.credentialName!);
  }

  // Check if the gateway supports HTTP traffic
  supportsHTTP(): boolean {
    return this.getServers().some(server => 
      server.port.protocol === 'HTTP' || server.port.protocol === 'HTTP2'
    );
  }

  // Check if the gateway supports HTTPS traffic
  supportsHTTPS(): boolean {
    return this.getServers().some(server => 
      server.port.protocol === 'HTTPS' || (server.tls && server.port.protocol === 'HTTP')
    );
  }

  // Get all unique port numbers
  getPortNumbers(): number[] {
    return [...new Set(this.getServers().map(server => server.port.number))];
  }
}