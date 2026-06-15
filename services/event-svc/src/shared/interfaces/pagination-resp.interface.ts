export interface IPaginationResponse {
  total: number;
  lastPage: number;
  currentPage: number;
  perPage: number;
  prev: number | null;
  next: number | null;
}

export interface GetPaginationResponse<T> {
  data: T[];
  meta: IPaginationResponse;
}
