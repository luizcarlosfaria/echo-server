using System;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.HttpOverrides;
using Microsoft.Extensions.Configuration;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;

namespace Jornada
{
    public class Startup
    {
        private IServiceProvider ApplicationServices;

        public Startup(IConfiguration configuration)
        {
            Configuration = configuration;
        }

        public IConfiguration Configuration { get; }


        const string redirectPath = "/redirect";
        private void CheckSameSite(HttpContext httpContext, CookieOptions options)
        {
            if (options.SameSite == SameSiteMode.None)
            {
                var userAgent = httpContext.Request.Headers["User-Agent"].ToString();
                // TODO: Use your User Agent library of choice here. 
                //if (/* UserAgent doesn’t support new behavior */)
                {
                    options.SameSite = SameSiteMode.Unspecified;
                }
            }
        }

        // This method gets called by the runtime. Use this method to add services to the container.
        public void ConfigureServices(IServiceCollection services)
        {

            services.Configure<ForwardedHeadersOptions>(options =>
            {
                options.ForwardedHeaders = ForwardedHeaders.All;
                options.KnownNetworks.Clear();
                options.KnownProxies.Clear();
            });


            services.AddControllersWithViews().AddRazorRuntimeCompilation();
        }


        // This method gets called by the runtime. Use this method to configure the HTTP request pipeline.
        public void Configure(IApplicationBuilder app, IWebHostEnvironment env)
        {
            //app.Use(async (context, next) =>
            //{
            //    try
            //    {
            //        await next.Invoke();
            //    }
            //    catch (Exception ex)
            //    {
            //        var requestId = System.Diagnostics.Activity.Current?.Id ?? context.TraceIdentifier;

            //        app.ApplicationServices.GetRequiredService<ILogger<Startup>>().LogCritical(ex, "Exception during request ID# {requestId} | {schema}://{host}{path}",
            //            requestId,
            //            context.Request.Scheme,
            //            context.Request.Host,
            //            context.Request.Path
            //        );

            //        app.ApplicationServices.GetRequiredService<ILogger<Startup>>().LogCritical(ex, "Exception during request ID# {requestId} | {excep[tion}",
            //            requestId,
            //            ex.ToString()
            //        );

            //    }
            //});


            if (env.IsDevelopment())
            {
                app.UseDeveloperExceptionPage();
            }
            else
            {
                app.UseExceptionHandler("/Home/Error");
            }

            app.UseStaticFiles();

            app.UseRouting();

            var fordwardedHeaderOptions = new ForwardedHeadersOptions
            {
                ForwardedHeaders = ForwardedHeaders.All
            };
            fordwardedHeaderOptions.KnownNetworks.Clear();
            fordwardedHeaderOptions.KnownProxies.Clear();

            app.UseForwardedHeaders(fordwardedHeaderOptions);

            app.UseCookiePolicy();
            //app.UseAuthentication();
            //app.UseAuthorization();

            app.UseEndpoints(endpoints =>
            {
                endpoints.MapControllerRoute(
                    name: "MyCatchAll",
                    pattern: "{*key}",
                    defaults: new { controller = "Default", action = "Index" }
                );

                endpoints.MapControllerRoute(
                    name: "default",
                    pattern: "{controller=Home}/{action=Index}/{id?}");
            });

           
        }



    }
}
