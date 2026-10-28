package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/service"
	"net/http"
)

// portfolioPhotosHandler 返回授权作品集内的照片。
func portfolioPhotosHandler(s *service.PortfolioPhotoService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		items, err := s.List(c.Request.Context(), claims.UserBusinessID, id)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"items": items})
	}
}

// portfolioUploadHandler 接收单张文件并限制实际读取字节数。
func portfolioUploadHandler(s *service.PortfolioPhotoService, maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := parseMultipartRequest(c, multipartBodyLimit(maxBytes, 1)); err != nil {
			writeError(c, err)
			return
		}
		defer removeMultipartTempFiles(c)
		form := c.Request.MultipartForm
		if err := validateMultipartKeys(form, nil, []string{"file"}); err != nil {
			writeError(c, err)
			return
		}
		files := form.File["file"]
		if len(form.File) != 1 || len(files) != 1 || len(form.Value) != 0 {
			writeError(c, fault.InvalidRequest("仅允许上传一张图片"))
			return
		}
		header := files[0]
		content, err := readValidatedImage(header, maxBytes)
		if err != nil {
			writeError(c, err)
			return
		}
		item, err := s.Upload(c.Request.Context(), claims.UserBusinessID, id, header.Filename, content)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, item)
	}
}

// publicPortfolioHandler 验证分享令牌后在作品集创建者范围内返回公开内容。
func publicPortfolioHandler(shares *service.PortfolioShareService, portfolios *service.PortfolioService, photos *service.PortfolioPhotoService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, item, err := shares.Resolve(c.Request.Context(), c.Param("share_token"))
		if err != nil {
			writeError(c, err)
			return
		}
		view, err := portfolios.Get(ctx, item.UserID, item.BusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		list, err := photos.List(ctx, item.UserID, item.BusinessID)
		if err != nil {
			writeError(c, err)
			return
		}
		tags, err := portfolios.PublicTags(ctx, item.UserID, view, list)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		writeData(c, http.StatusOK, gin.H{"portfolio": view, "photos": list, "tags": tags})
	}
}

// portfolioPhotoDeleteHandler 删除单张照片及其公开副本，失败时允许重试。
func portfolioPhotoDeleteHandler(s *service.PortfolioPhotoService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		photo, err := uuid.Parse(c.Param("photo_id"))
		if err != nil {
			writeError(c, fault.InvalidField("photo_id", "照片 ID 无效"))
			return
		}
		if err = s.Delete(c.Request.Context(), claims.UserBusinessID, id, photo); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"deleted": true})
	}
}

// portfolioPhotoUpdateHandler 更新照片的标题、标签和排序。
func portfolioPhotoUpdateHandler(s *service.PortfolioPhotoService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		photo, err := uuid.Parse(c.Param("photo_id"))
		if err != nil {
			writeError(c, fault.InvalidField("photo_id", "照片 ID 无效"))
			return
		}
		var input service.PortfolioPhotoInput
		if err = decodeJSON(c, &input); err != nil {
			writeError(c, err)
			return
		}
		item, err := s.Update(c.Request.Context(), claims.UserBusinessID, id, photo, input)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	}
}

// portfolioDeleteHandler 删除作品集、关联和公开照片。
func portfolioDeleteHandler(s *service.PortfolioPhotoService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		if err = s.DeletePortfolio(c.Request.Context(), claims.UserBusinessID, id); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusOK, gin.H{"deleted": true})
	}
}

// portfolioCopyHandler 将当前作者灵感图库中的照片复制到作品集。
func portfolioCopyHandler(s *service.PortfolioPhotoService) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := authContext(c)
		if err != nil {
			writeError(c, err)
			return
		}
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var input struct {
			InspirationID uuid.UUID `json:"inspiration_id"`
		}
		if err = decodeJSON(c, &input); err != nil {
			writeError(c, err)
			return
		}
		if input.InspirationID == uuid.Nil {
			writeError(c, fault.InvalidField("inspiration_id", "灵感图 ID 无效"))
			return
		}
		item, err := s.CopyInspiration(c.Request.Context(), claims.UserBusinessID, id, input.InspirationID)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, http.StatusCreated, item)
	}
}
