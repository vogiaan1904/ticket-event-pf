import { Options } from '@grpc/proto-loader';

// How every gRPC client decodes a reply. Without `defaults`, a proto3 zero value
// (false, 0, '', an unset message) arrives undefined, and a field the generated
// types declare present goes missing from the HTTP body.
export const GRPC_LOADER_OPTIONS: Options = { defaults: true };
