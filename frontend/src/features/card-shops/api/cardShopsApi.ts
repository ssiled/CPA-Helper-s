import { apiClient } from '@/shared/api/apiClient'
import type {
  CardShopFavoritesResponse,
  CardShopFavoriteUpdatePayload,
  CardShopProductStatusPayload,
  CardShopProductStatusResponse,
  CardShopTagsResponse,
  CardShopTagsUpdatePayload,
  CardShopsResponse,
} from '@/shared/types/api'

export function getCardShops(): Promise<CardShopsResponse> {
  return apiClient.get<CardShopsResponse>('/card-shops')
}

export function checkCardShopProductStatus(payload: CardShopProductStatusPayload): Promise<CardShopProductStatusResponse> {
  return apiClient.post<CardShopProductStatusResponse>('/card-shops/product-status', payload)
}

export function getCardShopFavorites(): Promise<CardShopFavoritesResponse> {
  return apiClient.get<CardShopFavoritesResponse>('/card-shops/favorites')
}

export function updateCardShopFavorite(payload: CardShopFavoriteUpdatePayload): Promise<CardShopFavoritesResponse> {
  return apiClient.put<CardShopFavoritesResponse>('/card-shops/favorites', payload)
}

export function getCardShopTags(): Promise<CardShopTagsResponse> {
  return apiClient.get<CardShopTagsResponse>('/card-shops/tags')
}

export function updateCardShopTags(payload: CardShopTagsUpdatePayload): Promise<CardShopTagsResponse> {
  return apiClient.put<CardShopTagsResponse>('/card-shops/tags', payload)
}
