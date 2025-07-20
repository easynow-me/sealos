/**
 * Copyright (c) OpenLens Authors. All rights reserved.
 * Licensed under MIT License. See LICENSE in root directory for more information.
 */

import type { NamespaceScopedMetadata } from '../api-types';
import { KubeObject } from '../kube-object';

export interface HTTPMatchRequest {
  uri?: {
    exact?: string;
    prefix?: string;
    regex?: string;
  };
  scheme?: {
    exact?: string;
    prefix?: string;
    regex?: string;
  };
  method?: {
    exact?: string;
    prefix?: string;
    regex?: string;
  };
  authority?: {
    exact?: string;
    prefix?: string;
    regex?: string;
  };
  headers?: {
    [key: string]: {
      exact?: string;
      prefix?: string;
      regex?: string;
    };
  };
  queryParams?: {
    [key: string]: {
      exact?: string;
      prefix?: string;
      regex?: string;
    };
  };
  ignoreUriCase?: boolean;
  withoutHeaders?: {
    [key: string]: {
      exact?: string;
      prefix?: string;
      regex?: string;
    };
  };
}

export interface Destination {
  host: string;
  subset?: string;
  port?: {
    number: number;
  };
}

export interface HTTPRouteDestination {
  destination: Destination;
  weight?: number;
  headers?: {
    request?: {
      set?: { [key: string]: string };
      add?: { [key: string]: string };
      remove?: string[];
    };
    response?: {
      set?: { [key: string]: string };
      add?: { [key: string]: string };
      remove?: string[];
    };
  };
}

export interface HTTPRetry {
  attempts?: number;
  perTryTimeout?: string;
  retryOn?: string;
  retryRemoteLocalities?: boolean;
}

export interface CorsPolicy {
  allowOrigins?: Array<{
    exact?: string;
    prefix?: string;
    regex?: string;
  }>;
  allowMethods?: string[];
  allowHeaders?: string[];
  exposeHeaders?: string[];
  maxAge?: string;
  allowCredentials?: boolean;
}

export interface HTTPFaultInjection {
  delay?: {
    percentage?: {
      value: number;
    };
    fixedDelay?: string;
    exponentialDelay?: string;
  };
  abort?: {
    percentage?: {
      value: number;
    };
    httpStatus?: number;
    grpcStatus?: string;
    http2Error?: string;
  };
}

export interface HTTPRoute {
  match?: HTTPMatchRequest[];
  route?: HTTPRouteDestination[];
  redirect?: {
    uri?: string;
    authority?: string;
    port?: number;
    derivePort?: 'FROM_PROTOCOL_DEFAULT' | 'FROM_REQUEST_PORT';
    scheme?: string;
    redirectCode?: number;
  };
  delegate?: {
    name: string;
    namespace?: string;
  };
  rewrite?: {
    uri?: string;
    authority?: string;
  };
  timeout?: string;
  retries?: HTTPRetry;
  fault?: HTTPFaultInjection;
  mirror?: Destination;
  mirrorPercentage?: {
    value: number;
  };
  corsPolicy?: CorsPolicy;
  headers?: {
    request?: {
      set?: { [key: string]: string };
      add?: { [key: string]: string };
      remove?: string[];
    };
    response?: {
      set?: { [key: string]: string };
      add?: { [key: string]: string };
      remove?: string[];
    };
  };
}

export interface VirtualServiceSpec {
  hosts: string[];
  gateways?: string[];
  http?: HTTPRoute[];
  tls?: any[]; // TLS configuration (simplified for now)
  tcp?: any[]; // TCP configuration (simplified for now)
  exportTo?: string[];
}

export interface VirtualServiceStatus {
  // Currently, Istio VirtualService doesn't have a complex status
  conditions?: {
    type: string;
    status: string;
    reason?: string;
    message?: string;
    lastTransitionTime?: string;
  }[];
}

export class VirtualService extends KubeObject<
  NamespaceScopedMetadata,
  VirtualServiceStatus,
  VirtualServiceSpec
> {
  static readonly kind = 'VirtualService';

  static readonly namespaced = true;

  static readonly apiBase = '/apis/networking.istio.io/v1beta1/virtualservices';

  getHosts(): string[] {
    return this.spec.hosts || [];
  }

  getGateways(): string[] {
    return this.spec.gateways || [];
  }

  getHTTPRoutes(): HTTPRoute[] {
    return this.spec.http || [];
  }

  // Get all destination hosts from all routes
  getDestinationHosts(): string[] {
    const destinations = new Set<string>();
    
    this.getHTTPRoutes().forEach(route => {
      route.route?.forEach(routeDestination => {
        destinations.add(routeDestination.destination.host);
      });
    });

    return Array.from(destinations);
  }

  // Get all destination services with their ports
  getDestinations(): Array<{ host: string; port?: number; weight?: number }> {
    const destinations: Array<{ host: string; port?: number; weight?: number }> = [];
    
    this.getHTTPRoutes().forEach(route => {
      route.route?.forEach(routeDestination => {
        destinations.push({
          host: routeDestination.destination.host,
          port: routeDestination.destination.port?.number,
          weight: routeDestination.weight
        });
      });
    });

    return destinations;
  }

  // Get all match conditions as readable strings
  getMatchConditions(): string[] {
    const conditions: string[] = [];
    
    this.getHTTPRoutes().forEach((route, index) => {
      route.match?.forEach((match, matchIndex) => {
        const parts: string[] = [];
        
        if (match.uri) {
          const uriCondition = match.uri.exact 
            ? `uri=${match.uri.exact}`
            : match.uri.prefix 
            ? `uri^=${match.uri.prefix}`
            : match.uri.regex 
            ? `uri~=${match.uri.regex}`
            : 'uri=*';
          parts.push(uriCondition);
        }
        
        if (match.method) {
          const methodCondition = match.method.exact || match.method.prefix || match.method.regex || '*';
          parts.push(`method=${methodCondition}`);
        }
        
        if (match.headers) {
          Object.entries(match.headers).forEach(([key, value]) => {
            const headerCondition = value.exact || value.prefix || value.regex || '*';
            parts.push(`${key}=${headerCondition}`);
          });
        }
        
        if (parts.length > 0) {
          conditions.push(`Route ${index + 1}.${matchIndex + 1}: ${parts.join(', ')}`);
        }
      });
    });

    return conditions;
  }

  // Check if this VirtualService has fault injection configured
  hasFaultInjection(): boolean {
    return this.getHTTPRoutes().some(route => route.fault);
  }

  // Check if this VirtualService has retries configured
  hasRetries(): boolean {
    return this.getHTTPRoutes().some(route => route.retries);
  }

  // Check if this VirtualService has CORS policy configured
  hasCorsPolicy(): boolean {
    return this.getHTTPRoutes().some(route => route.corsPolicy);
  }

  // Get all timeout values
  getTimeouts(): string[] {
    return this.getHTTPRoutes()
      .filter(route => route.timeout)
      .map(route => route.timeout!);
  }

  // Get summary of routing rules
  getRoutingSummary(): string[] {
    return this.getHTTPRoutes().map((route, index) => {
      const destinations = route.route?.map(r => `${r.destination.host}:${r.destination.port?.number || '*'}`).join(', ') || 'none';
      const matches = route.match?.length || 0;
      return `Route ${index + 1}: ${matches} match(es) → ${destinations}`;
    });
  }
}