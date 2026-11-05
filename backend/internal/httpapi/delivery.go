package httpapi

import (
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/service"
)

// registerDeliveryRoutes 注册创建者管理与客户能力链接，客户接口不接受空间和用户参数。
func registerDeliveryRoutes(api, protected *gin.RouterGroup, s *service.DeliveryService, maxUpload int64) {
	if s == nil {
		return
	}
	protected.GET("/deliveries", func(c *gin.Context) {
		items, err := s.List(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, gin.H{"items": items})
	})
	protected.POST("/deliveries", func(c *gin.Context) {
		var in service.DeliveryInput
		if err := decodeJSON(c, &in); err != nil {
			writeError(c, err)
			return
		}
		v, err := s.Create(c.Request.Context(), in)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 201, v)
	})
	protected.GET("/deliveries/:business_id", deliveryDetail(s, false))
	protected.POST("/deliveries/:business_id/actions", func(c *gin.Context) {
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		var in service.DeliveryCommand
		if err = decodeJSON(c, &in); err != nil {
			writeError(c, err)
			return
		}
		token, err := s.Command(c.Request.Context(), id, in)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, gin.H{"token": token})
	})
	protected.POST("/deliveries/:business_id/photos", func(c *gin.Context) {
		id, err := parseBusinessID(c)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUpload+multipartOverhead)
		if err = c.Request.ParseMultipartForm(multipartMemoryLimit); err != nil {
			writeError(c, fault.InvalidRequest("上传格式无效或超过大小限制"))
			return
		}
		defer c.Request.MultipartForm.RemoveAll()
		f, header, err := c.Request.FormFile("file")
		if err != nil {
			writeError(c, fault.InvalidRequest("缺少照片文件"))
			return
		}
		defer f.Close()
		version, err := strconv.Atoi(c.PostForm("version"))
		if err != nil {
			writeError(c, fault.InvalidRequest("缺少任务版本"))
			return
		}
		photo, err := s.Upload(c.Request.Context(), id, version, c.PostForm("kind"), header.Filename, c.PostForm("group_name"), f)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 201, gin.H{"business_id": photo})
	})
	protected.GET("/deliveries/:business_id/photos/:photo_id/file", deliveryFile(s, false))
	protected.POST("/deliveries/:business_id/exports", deliveryCreateExport(s, false))
	protected.GET("/deliveries/:business_id/exports/:job_id", deliveryExportStatus(s, false, false))
	protected.GET("/deliveries/:business_id/exports/:job_id/file", deliveryExportStatus(s, false, true))
	public := api.Group("/delivery-shares/:token")
	public.Use(func(c *gin.Context) {
		allowed, rateErr := s.AllowPublic(c.Request.Context(), c.ClientIP())
		if rateErr != nil {
			writeError(c, fault.Unavailable(rateErr))
			c.Abort()
			return
		}
		if !allowed {
			writeError(c, fault.New(429, "rate_limited", "访问过于频繁，请稍后重试", nil, nil))
			c.Abort()
			return
		}
		ctx, id, err := s.Resolve(c.Request.Context(), c.Param("token"), c.GetHeader("X-Delivery-Code"))
		if err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(ctx)
		c.Set("delivery_id", id)
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	})
	public.GET("", deliveryDetail(s, true))
	public.PUT("/selection", func(c *gin.Context) {
		id := c.MustGet("delivery_id").(uuid.UUID)
		var in service.DeliverySelection
		if err := decodeJSON(c, &in); err != nil {
			writeError(c, err)
			return
		}
		if err := s.Select(c.Request.Context(), id, in); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, gin.H{"saved": true})
	})
	public.POST("/receive", func(c *gin.Context) {
		if err := s.Receive(c.Request.Context(), c.MustGet("delivery_id").(uuid.UUID)); err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, gin.H{"received": true})
	})
	public.GET("/photos/:photo_id/file", deliveryFile(s, true))
	public.POST("/exports", deliveryCreateExport(s, true))
	public.GET("/exports/:job_id", deliveryExportStatus(s, true, false))
	public.GET("/exports/:job_id/file", deliveryExportStatus(s, true, true))
}

// deliveryCreateExport 返回 202 和作业 UUID，客户端应查询状态后下载。
func deliveryCreateExport(s *service.DeliveryService, public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := deliveryID(c, public)
		if err != nil {
			writeError(c, err)
			return
		}
		var in struct {
			Kind     string   `json:"kind"`
			PhotoIDs []string `json:"photo_ids"`
		}
		if err = decodeJSON(c, &in); err != nil {
			writeError(c, err)
			return
		}
		job, err := s.CreateExport(c.Request.Context(), id, in.Kind, public, in.PhotoIDs)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 202, job)
	}
}

// deliveryExportStatus 把状态查询与完成文件下载分开，下载仍逐次验证分享和任务代次。
func deliveryExportStatus(s *service.DeliveryService, public, file bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := deliveryID(c, public)
		if err != nil {
			writeError(c, err)
			return
		}
		job, err := uuid.Parse(c.Param("job_id"))
		if err != nil {
			writeError(c, fault.NotFound())
			return
		}
		if !file {
			result, err := s.ExportStatus(c.Request.Context(), id, job, public)
			if err != nil {
				writeError(c, err)
				return
			}
			writeData(c, 200, result)
			return
		}
		r, n, err := s.OpenExport(c.Request.Context(), id, job, public)
		if err != nil {
			writeError(c, err)
			return
		}
		defer r.Close()
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Disposition", "attachment; filename=delivery.zip")
		c.DataFromReader(200, n, "application/zip", r, nil)
	}
}

// deliveryID 只允许管理端解析业务 UUID，客户端能力路径使用中间件解析出的可信任务。
func deliveryID(c *gin.Context, public bool) (uuid.UUID, error) {
	if public {
		return c.MustGet("delivery_id").(uuid.UUID), nil
	}
	return parseBusinessID(c)
}
func deliveryDetail(s *service.DeliveryService, public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := deliveryID(c, public)
		if err != nil {
			writeError(c, err)
			return
		}
		item, err := s.Get(c.Request.Context(), id, public)
		if err != nil {
			writeError(c, err)
			return
		}
		writeData(c, 200, item)
	}
}

// deliveryFile 每次请求重新鉴权，并禁用缓存；下载仅使用服务端资产记录中的文件名。
func deliveryFile(s *service.DeliveryService, public bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := deliveryID(c, public)
		if err != nil {
			writeError(c, err)
			return
		}
		photo, err := uuid.Parse(c.Param("photo_id"))
		if err != nil {
			writeError(c, fault.NotFound())
			return
		}
		original := c.Query("original") == "1"
		if !original && c.Query("thumbnail") == "1" {
			r, n, err := s.OpenPhotoThumbnail(c.Request.Context(), id, photo, public)
			if err != nil {
				writeError(c, err)
				return
			}
			defer r.Close()
			c.Header("Cache-Control", "no-store")
			c.Header("X-Content-Type-Options", "nosniff")
			c.DataFromReader(200, n, "image/jpeg", r, nil)
			return
		}
		r, n, contentType, name, err := s.OpenPhoto(c.Request.Context(), id, photo, original, public)
		if err != nil {
			writeError(c, err)
			return
		}
		defer r.Close()
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		if original {
			c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		}
		c.DataFromReader(200, n, contentType, r, nil)
	}
}
