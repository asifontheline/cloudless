// Type definitions for the Cloudless JS/TS SDK.
export declare const VERSION: string;

export declare class CloudlessError extends Error {
  status: number;
  constructor(status: number, message: string);
}

export interface ClientOptions {
  apiKey?: string;
  timeoutMs?: number;
}

export type Message = { role: string; content: string };

export declare class Client {
  baseUrl: string;
  apiKey: string | null;
  timeoutMs: number;
  constructor(baseUrl?: string, opts?: ClientOptions);
  chat(messages: string | Message[], model?: string, params?: Record<string, unknown>): Promise<string>;
  completions(messages: string | Message[], model?: string, params?: Record<string, unknown>): Promise<any>;
  models(): Promise<any[]>;
  status(): Promise<any>;
  usage(): Promise<any>;
  ledger(): Promise<any>;
  capacity(): Promise<any>;
  audit(): Promise<any>;
  savings(promptPer1m?: number, completionPer1m?: number): Promise<any>;
  share(): Promise<any>;
  setShare(opts?: { cpuPercent?: number; shareWhen?: string }): Promise<any>;
  store(): Promise<any[]>;
  pull(name: string): Promise<any>;
}

export default Client;
