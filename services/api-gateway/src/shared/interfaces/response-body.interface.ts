export interface PaginationResponse {
  total: number;
  lastPage: number;
  currentPage: number;
  perPage: number;
  hasNext: boolean;
  hasPrevious: boolean;
}

export interface SuccessResponse<T> {
  success: true;
  message: string;
  data: T;
  meta?: unknown;
}

export interface ErrorResponse {
  success: false;
  message: string;
  code?: string;
  details?: unknown;
}
